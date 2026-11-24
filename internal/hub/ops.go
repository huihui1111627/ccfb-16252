package hub

import (
	"fmt"
	"time"

	"airport-baggage/internal/domain"
)

func startRetrievalLocked(b *domain.Bag, at domain.SimMinute) *domain.Retrieval {
	// Exact layer-aware handling time. A blind search is modelled at 40
	// minutes regardless of container; the digital record removes that.
	layerPenalty := map[int]float64{1: 58, 2: 28, 3: 8}[b.Layer]
	if layerPenalty == 0 {
		layerPenalty = 35
	}
	seconds := 28 + layerPenalty + float64(hashInt(b.Tag+"retrieve")%17)
	r := &domain.Retrieval{
		QueryAt:              at,
		PredictedContainerID: b.ContainerID,
		PredictedCode:        b.ContainerCode,
		PredictedLayer:       b.Layer,
		PredictedSlot:        b.Slot,
		Status:               "retrieving",
		StartedAt:            at,
		DigitalLocateMS:      0.08 + float64(hashInt(b.Tag+"ms")%90)/1000.0,
		BlindBaselineSeconds: 40 * 60,
	}
	b.Retrieval = r
	b.ReadyAt = at + seconds/60.0
	return r
}

// LocatePassengerBags returns exact container/layer/slot records and opens
// timed retrieval work for every bag already scanned into a container.
func (s *Service) LocatePassengerBags(paxID string, at domain.SimMinute) ([]*domain.Bag, time.Duration) {
	start := time.Now()
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	if p == nil {
		s.DB.Mu.Unlock()
		return nil, 0
	}
	out := []*domain.Bag{}
	for _, tag := range p.BagIDs {
		b := s.DB.Bags[tag]
		if b == nil {
			continue
		}
		out = append(out, b)
		if (b.Status == domain.BagLoaded || b.Status == domain.BagMisloaded) && b.Retrieval == nil {
			r := startRetrievalLocked(b, at)
			r.DigitalLocateMS = float64(time.Since(start).Microseconds()) / 1000.0
			if r.DigitalLocateMS <= 0 {
				r.DigitalLocateMS = 0.05
			}
			AddBagEvent(b, at, domain.EventLocate, fmt.Sprintf("中间件定位：%s 第%d层 %s，货位 %s", r.PredictedCode, r.PredictedLayer, domain.LayerLabel(r.PredictedLayer), r.PredictedSlot), "装载清单")
		}
	}
	s.DB.Mu.Unlock()
	elapsed := time.Since(start)
	if len(out) > 0 {
		first := out[0]
		if first.Retrieval != nil {
			s.AddAlert(at, "critical", TopicLoading, "未登机旅客行李已精确定位", fmt.Sprintf("%s 的 %d 件行李可按集装箱/层位直接卸载，无需翻找", p.Name, len(out)), "passenger", paxID)
		}
		s.RecordMessage(at, TopicLoading, "PASSENGER_BAG_LOCATE", paxID, map[string]interface{}{"bagCount": len(out)})
	}
	return out, elapsed
}

func (s *Service) CompleteRetrievals(at domain.SimMinute) int {
	tags := []string{}
	s.DB.Mu.RLock()
	for tag, b := range s.DB.Bags {
		if b.Retrieval != nil && b.Retrieval.CompletedAt == nil && at >= b.ReadyAt &&
			(b.Status == domain.BagLoaded || b.Status == domain.BagMisloaded) {
			tags = append(tags, tag)
		}
	}
	s.DB.Mu.RUnlock()
	for _, tag := range tags {
		s.completeRetrieval(tag, at)
	}
	return len(tags)
}

func (s *Service) completeRetrieval(tag string, at domain.SimMinute) {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil || b.Retrieval == nil || b.Retrieval.CompletedAt != nil || at < b.ReadyAt {
		s.DB.Mu.Unlock()
		return
	}
	r := b.Retrieval
	r.ActualContainerID = b.ContainerID
	r.ActualLayer = b.Layer
	r.ActualSlot = b.Slot
	r.Accurate = r.PredictedContainerID == b.ContainerID && r.PredictedLayer == b.Layer
	r.Status = "retrieved"
	// Timestamp completion at the exact modelled handling duration. The
	// simulation tick may discover the due item slightly after this instant.
	finishAt := b.ReadyAt
	if finishAt < r.StartedAt {
		finishAt = at
	}
	t := finishAt
	r.CompletedAt = &t
	r.ElapsedSeconds = (finishAt - r.StartedAt) * 60
	if r.ElapsedSeconds <= 0 {
		r.ElapsedSeconds = 1
	}
	removeBagFromContainerLocked(s, b)
	b.Status = domain.BagOffloaded
	b.OffloadedAt = &t
	b.ReadyAt = 0
	b.Reason = "未登机/取消旅客行李，按精确层位卸载"
	AddBagEvent(b, at, domain.EventRetrieve, fmt.Sprintf("装卸工在 %s 第%d层取出，实际用时 %.0f 秒", r.ActualContainerID, r.ActualLayer, r.ElapsedSeconds), r.ActualContainerID)
	msg := s.lockedMessage(at, TopicLoading, "RETRIEVAL_COMPLETED", tag, map[string]interface{}{
		"tag": tag, "accurate": r.Accurate, "elapsedSeconds": r.ElapsedSeconds,
	})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
}

func (s *Service) BoardPassenger(paxID string, at domain.SimMinute) {
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	if p == nil || p.Status == domain.PaxCancelled || p.Status == domain.PaxNoShow {
		s.DB.Mu.Unlock()
		return
	}
	p.Status = domain.PaxBoarded
	t := at
	p.BoardedAt = &t
	msg := s.lockedMessage(at, TopicDeparture, "PASSENGER_BOARDED", paxID, map[string]interface{}{"paxId": paxID, "flightId": p.FlightID})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
}

func (s *Service) MarkNoShow(paxID string, at domain.SimMinute) {
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	if p == nil || p.Status == domain.PaxBoarded || p.Status == domain.PaxCancelled {
		s.DB.Mu.Unlock()
		return
	}
	p.Status = domain.PaxNoShow
	msg := s.lockedMessage(at, TopicDeparture, "PASSENGER_NO_SHOW", paxID, map[string]interface{}{"paxId": paxID, "flightId": p.FlightID})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.LocatePassengerBags(paxID, at)
}

func (s *Service) CancelPassenger(paxID string, at domain.SimMinute, reason string) bool {
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	if p == nil || p.Status == domain.PaxCancelled {
		s.DB.Mu.Unlock()
		return false
	}
	p.Status = domain.PaxCancelled
	t := at
	p.CancelledAt = &t
	recallTags := []string{}
	retrievalTags := []string{}
	voidTags := []string{}
	for _, tag := range p.BagIDs {
		b := s.DB.Bags[tag]
		if b == nil {
			continue
		}
		switch {
		case b.Status == domain.BagLoaded || b.Status == domain.BagMisloaded:
			if b.Retrieval == nil {
				startRetrievalLocked(b, at)
			}
			b.Reason = "旅客取消，需从机上卸下"
			retrievalTags = append(retrievalTags, tag)
		case b.Status == domain.BagDeparted:
			b.Reason = "取消发生在起飞后，启动到达拦截"
		case b.Status == domain.BagPlanned:
			b.Status = domain.BagVoided
			b.VoidedAt = &t
			b.BSMReceived = false
			voidTags = append(voidTags, tag)
			AddBagEvent(b, at, domain.EventCancel, "行程取消，未发送的值机报文作废", "")
		default:
			b.Status = domain.BagRecallChute
			b.ChuteID = "RCL"
			b.Reason = "CHECKIN_VOID：立即从分拣线召回"
			b.ReadyAt = at + 1.1
			recallTags = append(recallTags, tag)
			AddBagEvent(b, at, domain.EventVoid, "收到值机撤销/行程取消，报文中止原路由并改派召回出口 RCL", "中间件")
		}
	}
	msg := s.lockedMessage(at, TopicCheckIn, "TRIP_CANCELLED_BSM_VOID", paxID, map[string]interface{}{
		"paxId": paxID, "recall": recallTags, "retrieve": retrievalTags, "voided": voidTags, "reason": reason,
	})
	note := fmt.Sprintf("已通知 %s：行程取消，%d 件分拣线上行李召回，%d 件已装车行李卸载", p.Name, len(recallTags), len(retrievalTags))
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.NotifyPassenger(at, paxID, "短信/航旅APP", "您的行程已取消，托运行李已同步撤销/拦截，工作人员将协助提取。")
	s.AddAlert(at, "warn", TopicCheckIn, "旅客取消，行李报文已作用到分拣线", note, "passenger", paxID)
	return true
}

func (s *Service) RebookPassenger(paxID, newFlightID string, at domain.SimMinute, reason string) bool {
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	nf := s.DB.Flights[newFlightID]
	if p == nil || nf == nil || p.Status == domain.PaxCancelled {
		s.DB.Mu.Unlock()
		return false
	}
	oldFlight := p.FlightID
	p.FlightID = newFlightID
	p.Status = domain.PaxRebooked
	p.RebookCount++
	p.Itinerary = append(p.Itinerary, newFlightID)
	reloaded := 0
	rerouted := 0
	for _, tag := range p.BagIDs {
		b := s.DB.Bags[tag]
		if b == nil {
			continue
		}
		if b.Status == domain.BagLoaded || b.Status == domain.BagMisloaded {
			removeBagFromContainerLocked(s, b)
			t := at
			b.OffloadedAt = &t
			reloaded++
			AddBagEvent(b, at, domain.EventRebook, fmt.Sprintf("旅客改签：从 %s 卸下，改挂 %s", oldFlight, newFlightID), b.ContainerCode)
		} else if b.Status != domain.BagDeparted && b.Status != domain.BagVoided {
			rerouted++
			AddBagEvent(b, at, domain.EventRebook, fmt.Sprintf("旅客改签：报文航段由 %s 改为 %s，分拣出口实时更新", oldFlight, newFlightID), "中间件")
		}
		b.FlightID = newFlightID
		b.NextFlightID = newFlightID
		b.Destination = nf.Destination
		b.ChuteID = nf.ChuteID
		b.ExpectedChuteID = nf.ChuteID
		b.Status = domain.BagBSMPublished
		b.BSMReceived = true
		b.ReadyAt = at + 2.0
		b.ContainerID = ""
		b.ContainerCode = ""
		b.Layer = 0
		b.LayerLabel = ""
		b.Slot = ""
		b.LoadedAt = nil
		b.Correct = nil
		b.ExpectedDisposition = fmt.Sprintf("depart_on_%s", newFlightID)
	}
	msg := s.lockedMessage(at, TopicTransfer, "PASSENGER_REBOOKED_BAG_UPDATED", paxID, map[string]interface{}{
		"paxId": paxID, "oldFlightId": oldFlight, "newFlightId": newFlightID,
		"offloadedBags": reloaded, "reroutedBags": rerouted, "reason": reason,
	})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.NotifyPassenger(at, paxID, "短信/航旅APP", fmt.Sprintf("您已改签至 %s %s，行李报文已同步改挂，无需重新托运。", nf.Number, domain.ClockString(nf.DepartAt)))
	s.AddAlert(at, "info", TopicTransfer, "旅客改签，行李跟随改挂", fmt.Sprintf("%s 改乘 %s；%d 件已卸重挂，%d 件在分拣线改派出口", p.Name, nf.Number, reloaded, rerouted), "passenger", paxID)
	return true
}

type FlightChangeResult struct {
	NotLoaded int      `json:"notLoaded"`
	Loaded    int      `json:"loaded"`
	BagTags   []string `json:"bagTags"`
}

func (s *Service) ChangeGate(flightID, newGate, newChute, reason string, at domain.SimMinute) FlightChangeResult {
	res := FlightChangeResult{}
	s.DB.Mu.Lock()
	f := s.DB.Flights[flightID]
	if f == nil {
		s.DB.Mu.Unlock()
		return res
	}
	oldGate := f.Gate
	oldChute := f.ChuteID
	f.GateHistory = append(f.GateHistory, domain.GateChange{At: at, OldGate: oldGate, NewGate: newGate, Reason: reason})
	f.Gate = newGate
	f.ChuteID = newChute
	for _, b := range s.DB.Bags {
		if b.FlightID != flightID {
			continue
		}
		switch b.Status {
		case domain.BagLoaded, domain.BagMisloaded:
			res.Loaded++
			AddBagEvent(b, at, domain.EventGateChange, fmt.Sprintf("登机口 %s→%s；行李已装入 %s，通知装卸/配载按新机位保障", oldGate, newGate, b.ContainerCode), b.ContainerCode)
		case domain.BagDeparted, domain.BagOffloaded, domain.BagVoided:
			continue
		default:
			res.NotLoaded++
			res.BagTags = append(res.BagTags, b.Tag)
			b.ChuteID = newChute
			b.ExpectedChuteID = newChute
			if b.Status == domain.BagAtChute || b.Status == domain.BagChuteAssigned {
				b.Status = domain.BagChuteAssigned
				b.ReadyAt = at + 0.4
			}
			AddBagEvent(b, at, domain.EventGateChange, fmt.Sprintf("登机口 %s→%s；未装车，分拣路由 %s→%s", oldGate, newGate, oldChute, newChute), "中间件")
		}
	}
	msg := s.lockedMessage(at, TopicOperations, "FLIGHT_GATE_CHANGED", flightID, map[string]interface{}{
		"oldGate": oldGate, "newGate": newGate, "oldChute": oldChute, "newChute": newChute,
		"rerouted": res.NotLoaded, "loadedNotified": res.Loaded,
	})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.AddAlert(at, "warn", TopicOperations, "航班换登机口，行李路由已更新", fmt.Sprintf("%s %s→%s：%d 件未装车改去 %s，%d 件已装车通知装卸", f.Number, oldGate, newGate, res.NotLoaded, newChute, res.Loaded), "flight", flightID)
	return res
}

func (s *Service) ChangeAircraft(flightID, newReg, newModel, newChute, reason string, at domain.SimMinute) FlightChangeResult {
	res := FlightChangeResult{}
	s.DB.Mu.Lock()
	f := s.DB.Flights[flightID]
	if f == nil {
		s.DB.Mu.Unlock()
		return res
	}
	oldReg, oldModel, oldChute := f.AircraftReg, f.AircraftModel, f.ChuteID
	f.AircraftHistory = append(f.AircraftHistory, domain.AircraftChange{At: at, OldReg: oldReg, NewReg: newReg, OldModel: oldModel, NewModel: newModel, Reason: reason})
	f.AircraftReg = newReg
	f.AircraftModel = newModel
	if newChute != "" {
		f.ChuteID = newChute
	}
	for _, b := range s.DB.Bags {
		if b.FlightID != flightID {
			continue
		}
		switch b.Status {
		case domain.BagLoaded, domain.BagMisloaded:
			res.Loaded++
			AddBagEvent(b, at, domain.EventAircraftChange, fmt.Sprintf("飞机 %s(%s)→%s(%s)；已装集装箱 %s 保留，通知装卸/配载核对舱位", oldReg, oldModel, newReg, newModel, b.ContainerCode), b.ContainerCode)
		case domain.BagDeparted, domain.BagOffloaded, domain.BagVoided:
			continue
		default:
			res.NotLoaded++
			b.ChuteID = f.ChuteID
			b.ExpectedChuteID = f.ChuteID
			if b.Status == domain.BagAtChute || b.Status == domain.BagChuteAssigned {
				b.Status = domain.BagChuteAssigned
				b.ReadyAt = at + 0.4
			}
			AddBagEvent(b, at, domain.EventAircraftChange, fmt.Sprintf("换机 %s→%s；未装车行李改派出口 %s", oldReg, newReg, f.ChuteID), "中间件")
		}
	}
	msg := s.lockedMessage(at, TopicOperations, "FLIGHT_AIRCRAFT_CHANGED", flightID, map[string]interface{}{
		"oldReg": oldReg, "newReg": newReg, "oldChute": oldChute, "newChute": f.ChuteID,
		"rerouted": res.NotLoaded, "loadedNotified": res.Loaded,
	})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.AddAlert(at, "warn", TopicOperations, "航班换飞机，行李保障已联动", fmt.Sprintf("%s 换机为 %s；%d 件未装车改出口，%d 件已装车收到装卸核对通知", f.Number, newReg, res.NotLoaded, res.Loaded), "flight", flightID)
	return res
}
