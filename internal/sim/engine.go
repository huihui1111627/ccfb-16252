package sim

import (
	"context"
	"fmt"
	"sync"
	"time"

	"airport-baggage/internal/domain"
	"airport-baggage/internal/hub"
	"airport-baggage/internal/middleware"
	"airport-baggage/internal/store"
)

type Engine struct {
	mu      sync.RWMutex
	tickMu  sync.Mutex
	db      *store.Store
	svc     *hub.Service
	plan    *Plan
	now     domain.SimMinute
	running bool
	speed   float64
	seed    int64

	done         map[string]bool
	remediateAt  map[string]domain.SimMinute
	recheckAt    map[string]domain.SimMinute
	lastAudit    domain.SimMinute
	finalAudited bool
}

func NewEngine(seed int64) *Engine {
	db, plan := Generate(seed)
	bus := middleware.NewBus()
	svc := hub.NewService(db, bus)
	return &Engine{
		db: db, svc: svc, plan: plan, seed: seed,
		now: domain.DayStart, running: true, speed: 18,
		done: map[string]bool{}, remediateAt: map[string]domain.SimMinute{}, recheckAt: map[string]domain.SimMinute{},
	}
}

func (e *Engine) DB() *store.Store {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.db
}

func (e *Engine) Service() *hub.Service {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.svc
}

func (e *Engine) Plan() *Plan {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.plan
}

func (e *Engine) Clock() domain.SimMinute {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.now
}

func (e *Engine) Runtime() (now domain.SimMinute, running bool, speed float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.now, e.running, e.speed
}

func (e *Engine) SetRunning(v bool) {
	e.mu.Lock()
	e.running = v
	e.mu.Unlock()
}

func (e *Engine) Toggle() bool {
	e.mu.Lock()
	e.running = !e.running
	v := e.running
	e.mu.Unlock()
	return v
}

func (e *Engine) SetSpeed(v float64) {
	if v <= 0 {
		v = 1
	}
	e.mu.Lock()
	e.speed = v
	e.running = true
	e.mu.Unlock()
}

func (e *Engine) FinishDay() {
	e.mu.Lock()
	e.speed = 720
	e.running = true
	e.mu.Unlock()
}

func (e *Engine) Reset() {
	db, plan := Generate(e.seed)
	bus := middleware.NewBus()
	svc := hub.NewService(db, bus)
	e.mu.Lock()
	e.db = db
	e.svc = svc
	e.plan = plan
	e.now = domain.DayStart
	e.running = true
	e.speed = 18
	e.done = map[string]bool{}
	e.remediateAt = map[string]domain.SimMinute{}
	e.recheckAt = map[string]domain.SimMinute{}
	e.lastAudit = 0
	e.finalAudited = false
	e.mu.Unlock()
}

func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, running, speed := e.Runtime()
			if running {
				// 100ms wall tick. Speed is simulated minutes per real second.
				e.Tick(speed * 0.1)
			}
		}
	}
}

func (e *Engine) Tick(dt domain.SimMinute) {
	e.tickMu.Lock()
	defer e.tickMu.Unlock()

	e.mu.Lock()
	e.now += dt
	now := e.now
	svc := e.svc
	e.mu.Unlock()

	e.runDirector(now, svc)
	e.processCheckIns(now, svc)
	e.activateTransfers(now, svc)
	e.processSorter(now, svc)
	e.processBoarding(now)
	e.processLoading(now, svc)
	svc.CompleteRetrievals(now)
	e.processFlights(now, svc)

	if now-e.lastAudit >= 4 {
		svc.AuditFinalDispositions(now)
		e.mu.Lock()
		e.lastAudit = now
		e.mu.Unlock()
	}
	if now >= domain.DayEnd {
		if !e.finalAudited {
			svc.AuditFinalDispositions(now)
			e.mu.Lock()
			e.finalAudited = true
			e.now = domain.DayEnd
			e.running = false
			e.mu.Unlock()
		} else {
			e.mu.Lock()
			e.now = domain.DayEnd
			e.running = false
			e.mu.Unlock()
		}
	}
}

func (e *Engine) processCheckIns(now domain.SimMinute, svc *hub.Service) {
	db := e.DB()
	ids := []string{}
	db.Mu.RLock()
	for id, p := range db.Passengers {
		if p.Status == domain.PaxBooked && p.CheckedInAt != nil && *p.CheckedInAt <= now {
			ids = append(ids, id)
			if len(ids) >= 420 {
				break
			}
		}
	}
	db.Mu.RUnlock()
	for _, id := range ids {
		svc.CheckIn(id, now)
	}
}

func (e *Engine) activateTransfers(now domain.SimMinute, svc *hub.Service) {
	db := e.DB()
	activated := []string{}
	db.Mu.Lock()
	for tag, b := range db.Bags {
		if b.Status != domain.BagTransferHold || b.InboundFlightID == "" {
			continue
		}
		ib := db.Flights[b.InboundFlightID]
		if ib == nil || now < ib.EstimatedAt+16 {
			continue
		}
		b.Status = domain.BagInducted
		b.BSMReceived = true
		b.ReadyAt = 0
		t := now
		b.InductedAt = &t
		b.Progress = 0.32
		hub.AddBagEvent(b, now, domain.EventTransfer, "中转航班到达，行李卸下并进入中转分拣线", ib.ID)
		activated = append(activated, tag)
		if len(activated) >= 600 {
			break
		}
	}
	db.Mu.Unlock()
	if len(activated) > 0 {
		svc.RecordMessage(now, hub.TopicTransfer, "TRANSFER_BAGS_INDUCTED", "batch", map[string]interface{}{"count": len(activated), "first": activated[:min(8, len(activated))]})
	}
}

func (e *Engine) processSorter(now domain.SimMinute, svc *hub.Service) {
	db := e.DB()
	induct := []string{}
	scan := []string{}
	arrive := []string{}
	recall := []string{}
	db.Mu.RLock()
	for tag, b := range db.Bags {
		switch b.Status {
		case domain.BagBSMPublished:
			if b.ReadyAt <= now && len(induct) < 320 {
				induct = append(induct, tag)
			}
		case domain.BagInducted, domain.BagScanning:
			if b.ReadyAt <= now && len(scan) < 900 {
				scan = append(scan, tag)
			}
		case domain.BagChuteAssigned:
			if b.ReadyAt <= now && len(arrive) < 1200 {
				arrive = append(arrive, tag)
			}
		case domain.BagRecallChute:
			if b.ReadyAt <= now {
				recall = append(recall, tag)
			}
		}
	}
	db.Mu.RUnlock()

	for _, tag := range induct {
		svc.InductBag(tag, now)
	}
	for _, tag := range scan {
		if svc.ScanBag(tag, now) {
			svc.AssessTransfer(tag, now)
		}
	}
	for _, tag := range arrive {
		svc.ArriveChute(tag, now)
	}
	for _, tag := range recall {
		svc.CompleteRecall(tag, now)
	}
}

func boardingMinute(p *domain.Passenger, f *domain.Flight) domain.SimMinute {
	h := 0
	for _, ch := range p.ID {
		h = h*31 + int(ch)
	}
	if h < 0 {
		h = -h
	}
	return f.BoardingAt + float64(h%14)
}

func (e *Engine) processBoarding(now domain.SimMinute) {
	db := e.DB()
	svc := e.Service()
	ids := []string{}
	statusUpdates := map[string]domain.FlightStatus{}
	db.Mu.RLock()
	for _, f := range db.Flights {
		if f.Kind != "outbound" || f.Status == domain.FlightDeparted || f.Status == domain.FlightBlocked {
			continue
		}
		if now >= f.FinalCallAt {
			statusUpdates[f.ID] = domain.FlightFinalCall
		} else if now >= f.BoardingAt {
			statusUpdates[f.ID] = domain.FlightBoarding
		}
		if now < f.BoardingAt {
			continue
		}
		for _, p := range db.Passengers {
			if p.FlightID != f.ID {
				continue
			}
			if p.Special {
				continue
			}
			if p.Status != domain.PaxCheckedIn && p.Status != domain.PaxRebooked {
				continue
			}
			if now >= boardingMinute(p, f) && len(ids) < 1400 {
				ids = append(ids, p.ID)
			}
		}
	}
	db.Mu.RUnlock()
	db.Mu.Lock()
	for id, st := range statusUpdates {
		if f := db.Flights[id]; f != nil && f.Status != domain.FlightDeparted && f.Status != domain.FlightBlocked {
			f.Status = st
		}
	}
	db.Mu.Unlock()
	for _, id := range ids {
		svc.BoardPassenger(id, now)
	}
}

type loadCandidate struct {
	tag     string
	forced  bool
	departs domain.SimMinute
}

func (e *Engine) processLoading(now domain.SimMinute, svc *hub.Service) {
	db := e.DB()
	candidates := []loadCandidate{}
	db.Mu.RLock()
	for tag, b := range db.Bags {
		if b.Status != domain.BagAtChute {
			continue
		}
		f := db.Flights[b.FlightID]
		if f == nil || f.Kind != "outbound" {
			continue
		}
		if now < f.DepartAt-78 {
			continue
		}
		candidates = append(candidates, loadCandidate{tag: tag, forced: b.ForceContainerID != "", departs: f.DepartAt})
	}
	db.Mu.RUnlock()
	// Forced scenario bags load first, then earliest departures.
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].forced && !candidates[i].forced {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	count := 0
	for _, c := range candidates {
		if count >= 520 {
			break
		}
		if svc.LoadBag(c.tag, now) {
			count++
		}
	}
}

func (e *Engine) processFlights(now domain.SimMinute, svc *hub.Service) {
	db := e.DB()
	ids := []string{}
	db.Mu.RLock()
	for id, f := range db.Flights {
		if f.Kind == "outbound" {
			ids = append(ids, id)
		}
	}
	db.Mu.RUnlock()

	for _, id := range ids {
		f := db.Flights[id]
		if f == nil {
			continue
		}
		if len(f.Reconciliations) == 0 && now >= f.ReconcileAt {
			rec := svc.ReconcileFlight(id, now)
			if rec != nil && rec.Passed {
				svc.ReleaseFlight(id, now)
			} else if rec != nil {
				e.mu.Lock()
				e.remediateAt[id] = now + 1.4
				e.mu.Unlock()
			}
		}

		e.mu.Lock()
		remAt, hasRem := e.remediateAt[id]
		if hasRem && now >= remAt {
			delete(e.remediateAt, id)
			e.recheckAt[id] = now + 1.8
		}
		reAt, hasRe := e.recheckAt[id]
		if hasRe && now >= reAt {
			delete(e.recheckAt, id)
		}
		e.mu.Unlock()

		if hasRem && now >= remAt {
			svc.RemediateFlight(id, now)
		}
		if hasRe && now >= reAt {
			rec := svc.ReconcileFlight(id, now)
			if rec != nil && rec.Passed {
				svc.ReleaseFlight(id, now)
				if now >= f.DepartAt {
					svc.DepartFlight(id, now)
				}
			} else if rec != nil {
				svc.RemediateFlight(id, now)
				e.mu.Lock()
				e.recheckAt[id] = now + 1.4
				e.mu.Unlock()
			}
		}

		if now >= f.DepartAt {
			db.Mu.RLock()
			released := f.Released
			status := f.Status
			db.Mu.RUnlock()
			if released && status != domain.FlightDeparted {
				svc.DepartFlight(id, now)
			}
		}
	}
}

func (e *Engine) runDirector(now domain.SimMinute, svc *hub.Service) {
	plan := e.Plan()
	for _, item := range plan.Items {
		e.mu.Lock()
		if e.done[item.ID] || now < item.At {
			e.mu.Unlock()
			continue
		}
		e.done[item.ID] = true
		e.mu.Unlock()
		e.executeItem(item, now, svc)
	}
}

func (e *Engine) executeItem(item DirectorItem, now domain.SimMinute, svc *hub.Service) {
	if item.ScenarioID != "" {
		svc.SetScenario(item.ScenarioID, "running", "演练事件已触发", "", nil)
	}
	db := e.DB()
	switch item.Kind {
	case "orphan_scan":
		svc.HandleUnknownPhysicalScan(item.RefID, now)
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "未知标签已拦截到 EXC 并提醒值机核实", "", &t)
		}
	case "resolve_orphan":
		svc.ResolveUnknownScan(item.RefID, now, item.RefID2, "值机核实补录旅客")
	case "gate_change":
		svc.ChangeGate(item.RefID, item.NewGate, item.NewChute, item.Text, now)
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "未装车行李改派新出口，已装车行李收到通知", "", &t)
		}
	case "aircraft_change":
		svc.ChangeAircraft(item.RefID, item.NewReg, item.NewModel, item.NewChute, item.Text, now)
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "换机联动完成：路由/装卸通知均已更新", "", &t)
		}
	case "cancel_pax":
		e.forceFirstBagOnSorter(item.RefID, now)
		svc.CancelPassenger(item.RefID, now, item.Text)
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "撤销报文已作用到正在分拣线上的行李", "", &t)
		}
	case "rebook_pax":
		svc.RebookPassenger(item.RefID, item.RefID2, now, item.Text)
		db.Mu.Lock()
		if p := db.Passengers[item.RefID]; p != nil {
			p.Special = false
		}
		db.Mu.Unlock()
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "旅客与行李已同步改挂新航班", "", &t)
		}
	case "no_show":
		svc.MarkNoShow(item.RefID, now)
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "未登机判定后立即生成集装箱/层位定位与卸载计时", "", &t)
		}
	case "delay_inbound":
		db.Mu.Lock()
		f := db.Flights[item.RefID]
		if f != nil {
			f.EstimatedAt = f.ScheduledAt + item.Value
			f.DelayMinutes = item.Value
			f.Status = domain.FlightDelayed
		}
		db.Mu.Unlock()
		svc.AssessTransfersForInbound(item.RefID, now)
		svc.RecordMessage(now, hub.TopicOperations, "INBOUND_DELAY", item.RefID, map[string]interface{}{"delayMinutes": item.Value})
		svc.AddAlert(now, "warn", hub.TopicTransfer, "中转前段延误", item.RefID+" 延误 "+formatMinutes(item.Value)+"，中转行李将自动重算", "flight", item.RefID)
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "passed", "延误已纳入 MCT 重算，来不及的行李自动保护改签", "", &t)
		}
	case "inject_problem":
		tag := svc.InjectDoorFault(item.RefID, item.Text, now)
		if tag == "" {
			e.forceAnyBagLoaded(item.RefID, now, svc)
			tag = svc.InjectDoorFault(item.RefID, item.Text, now)
		}
		if item.ScenarioID != "" {
			t := now
			svc.SetScenario(item.ScenarioID, "running", "缺陷 "+tag+" 已注入，等待关舱核对拦截", "", nil)
			_ = t
		}
	}
}

func formatMinutes(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

func (e *Engine) forceFirstBagOnSorter(paxID string, now domain.SimMinute) {
	db := e.DB()
	db.Mu.Lock()
	p := db.Passengers[paxID]
	if p == nil || len(p.BagIDs) == 0 {
		db.Mu.Unlock()
		return
	}
	if p.Status == domain.PaxBooked {
		p.Status = domain.PaxCheckedIn
		p.CheckedInAt = &now
	}
	b := db.Bags[p.BagIDs[0]]
	if b != nil && (b.Status == domain.BagPlanned || b.Status == domain.BagBSMPublished) {
		b.Status = domain.BagInducted
		b.BSMReceived = true
		t := now
		b.InductedAt = &t
		b.Progress = 0.35
		b.ReadyAt = now + 8
		hub.AddBagEvent(b, now, domain.EventInduct, "演练时刻：行李正位于分拣线扫描段之前", "分拣线")
	}
	db.Mu.Unlock()
}

func (e *Engine) forceAnyBagLoaded(flightID string, now domain.SimMinute, svc *hub.Service) {
	db := e.DB()
	var tag string
	db.Mu.RLock()
	for t, b := range db.Bags {
		if b.FlightID == flightID {
			tag = t
			break
		}
	}
	db.Mu.RUnlock()
	if tag == "" {
		return
	}
	db.Mu.Lock()
	if b := db.Bags[tag]; b != nil {
		b.Status = domain.BagAtChute
		b.ReadyAt = 0
	}
	db.Mu.Unlock()
	_ = svc.LoadBag(tag, now)
}
