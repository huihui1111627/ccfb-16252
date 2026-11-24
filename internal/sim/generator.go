package sim

import (
	"fmt"
	"math/rand"
	"sort"

	"airport-baggage/internal/domain"
	"airport-baggage/internal/store"
)

type destination struct {
	City, Code string
	Intl       bool
}

var destinations = []destination{
	{"北京", "PEK", false}, {"上海", "PVG", false}, {"广州", "CAN", false}, {"深圳", "SZX", false},
	{"成都", "TFU", false}, {"重庆", "CKG", false}, {"杭州", "HGH", false}, {"西安", "XIY", false},
	{"南京", "NKG", false}, {"昆明", "KMG", false}, {"长沙", "CSX", false}, {"青岛", "TAO", false},
	{"厦门", "XMN", false}, {"乌鲁木齐", "URC", false}, {"哈尔滨", "HRB", false}, {"三亚", "SYX", false},
	{"海口", "HAK", false}, {"贵阳", "KWE", false}, {"兰州", "LHW", false}, {"沈阳", "SHE", false},
	{"大连", "DLC", false}, {"郑州", "CGO", false}, {"武汉", "WUH", false}, {"合肥", "HFE", false},
	{"福州", "FOC", false}, {"济南", "TNA", false}, {"南宁", "NNG", false}, {"拉萨", "LXA", false},
	{"香港", "HKG", true}, {"台北", "TPE", true}, {"东京", "NRT", true}, {"首尔", "ICN", true},
	{"新加坡", "SIN", true}, {"曼谷", "BKK", true}, {"迪拜", "DXB", true}, {"悉尼", "SYD", true},
	{"巴黎", "CDG", true}, {"法兰克福", "FRA", true}, {"洛杉矶", "LAX", true}, {"纽约", "JFK", true},
}

var surnames = []string{"王", "李", "张", "刘", "陈", "杨", "赵", "黄", "周", "吴", "徐", "孙", "胡", "朱", "高", "林", "何", "郭", "马", "罗", "梁", "宋", "郑", "谢", "韩", "唐", "冯", "于", "董", "萧", "程", "曹", "袁", "邓", "许", "傅", "沈", "曾", "彭", "吕"}
var given = []string{"伟", "芳", "娜", "敏", "静", "磊", "洋", "艳", "勇", "军", "杰", "娟", "涛", "明", "超", "秀英", "霞", "平", "刚", "桂英", "子轩", "梓涵", "宇辰", "一诺", "思远", "雨桐", "浩然", "欣怡", "嘉禾", "若曦", "建国", "志强", "海燕", "晓东", "文博", "梦琪"}

var airlineCodes = []struct{ Code, Name string }{{"CA", "国航"}, {"MU", "东航"}, {"CZ", "南航"}, {"HU", "海航"}, {"MF", "厦航"}}

func Generate(seed int64) (*store.Store, *Plan) {
	r := rand.New(rand.NewSource(seed))
	db := store.New()
	plan := &Plan{}

	for i := 1; i <= 36; i++ {
		id := fmt.Sprintf("C%02d", i)
		db.Chutes[id] = &domain.Chute{ID: id, Name: fmt.Sprintf("%d号分拣出口", i), Kind: "flight", Status: "normal"}
	}
	db.Chutes["EXC"] = &domain.Chute{ID: "EXC", Name: "7号异常/无报文出口", Kind: "exception", Status: "exception"}
	db.Chutes["RCL"] = &domain.Chute{ID: "RCL", Name: "撤销召回出口", Kind: "recall", Status: "reroute"}

	// Inbound wave: arrivals through the day feed transfer bags.
	inboundCount := 76
	for i := 0; i < inboundCount; i++ {
		arr := 390 + float64(i)*9.6 + r.Float64()*2.5
		d := destinations[r.Intn(28)]
		al := airlineCodes[r.Intn(len(airlineCodes))]
		id := fmt.Sprintf("IN%03d", i+1)
		f := &domain.Flight{
			ID: id, Number: fmt.Sprintf("%s%04d", al.Code, 2100+i), Airline: al.Name, Kind: "inbound",
			Origin: d.City, Destination: "中州枢纽", International: d.Intl,
			Gate: fmt.Sprintf("D%02d", 1+r.Intn(12)), Stand: fmt.Sprintf("N%02d", 20+r.Intn(30)),
			AircraftReg: fmt.Sprintf("B-%04d", 3000+i), AircraftModel: []string{"A320", "B738", "A321"}[r.Intn(3)],
			ScheduledAt: arr, EstimatedAt: arr, ArrivalAt: arr, Status: domain.FlightScheduled,
			Capacity: 150 + r.Intn(60),
		}
		db.Flights[id] = f
	}

	outboundCount := 138
	for i := 0; i < outboundCount; i++ {
		dep := 420 + float64(i)*7.15 + r.Float64()*1.5
		d := destinations[r.Intn(len(destinations))]
		al := airlineCodes[r.Intn(len(airlineCodes))]
		wide := r.Float64() < 0.22 || d.Intl
		capacity := 150 + r.Intn(45)
		model := []string{"A320neo", "B737-800", "A321neo"}[r.Intn(3)]
		containers := 6 + r.Intn(3)
		if wide {
			capacity = 230 + r.Intn(70)
			model = []string{"B787-9", "A330-300", "B777-300ER"}[r.Intn(3)]
			containers = 12 + r.Intn(7)
		}
		id := fmt.Sprintf("OUT%03d", i+1)
		chute := fmt.Sprintf("C%02d", i%36+1)
		gate := fmt.Sprintf("%s%02d", []string{"A", "B", "C"}[i%3], 1+(i*7)%22)
		f := &domain.Flight{
			ID: id, Number: fmt.Sprintf("%s%04d", al.Code, 1000+i), Airline: al.Name, Kind: "outbound",
			Origin: "中州枢纽", Destination: d.City, International: d.Intl,
			Gate: gate, ChuteID: chute, Stand: fmt.Sprintf("S%02d", 1+(i*3)%48),
			AircraftReg: fmt.Sprintf("B-%04d", 1000+i*7%8900), AircraftModel: model,
			ContainerPlan: containers, ScheduledAt: dep, EstimatedAt: dep,
			Status: domain.FlightScheduled, Capacity: capacity,
			BoardingAt: dep - 30, FinalCallAt: dep - 16, ReconcileAt: dep - 10, DepartAt: dep,
		}
		db.Flights[id] = f
		for ci := 0; ci < containers; ci++ {
			cid := fmt.Sprintf("%s-CTR-%02d", id, ci+1)
			cap := 34 + r.Intn(12)
			if wide {
				cap = 42 + r.Intn(10)
			}
			code := fmt.Sprintf("AKE%05dCA", 50000+len(db.Containers)+ci)
			if ci == containers-1 && !wide {
				code = fmt.Sprintf("BULK-%s", id)
				cap = 70
			}
			c := &domain.Container{ID: cid, Code: code, FlightID: id, Kind: "AKE", Index: ci + 1, Capacity: cap}
			if ci == containers-1 && !wide {
				c.Kind = "bulk"
			}
			db.Containers[cid] = c
			f.Containers = append(f.Containers, cid)
		}
	}

	flightIDs := db.FlightList()
	outbound := []*domain.Flight{}
	inbound := []*domain.Flight{}
	for _, f := range flightIDs {
		if f.Kind == "outbound" {
			outbound = append(outbound, f)
		} else {
			inbound = append(inbound, f)
		}
	}
	sort.Slice(outbound, func(i, j int) bool { return outbound[i].ScheduledAt < outbound[j].ScheduledAt })

	bagSeq := 100000
	paxSeq := 1
	pnrSeq := 0
	for _, f := range outbound {
		loadFactor := 0.76 + r.Float64()*0.18
		paxCount := int(float64(f.Capacity) * loadFactor)
		for j := 0; j < paxCount; j++ {
			paxID := fmt.Sprintf("P%06d", paxSeq)
			paxSeq++
			pnrSeq++
			pnr := fmt.Sprintf("%s%03d", randomPNR(r, 3), pnrSeq%1000)
			name := surnames[r.Intn(len(surnames))] + given[r.Intn(len(given))]
			p := &domain.Passenger{
				ID: paxID, PNR: pnr, Name: name, Phone: fmt.Sprintf("138%08d", r.Intn(100000000)),
				FlightID: f.ID, OriginalFlightID: f.ID, Itinerary: []string{f.ID},
			}
			if r.Float64() < 0.29 && len(inbound) > 0 {
				var ib *domain.Flight
				for tries := 0; tries < 12; tries++ {
					cand := inbound[r.Intn(len(inbound))]
					if cand.ScheduledAt <= f.ScheduledAt-52 && cand.ScheduledAt >= f.ScheduledAt-390 {
						ib = cand
						break
					}
				}
				if ib != nil {
					p.InboundFlightID = ib.ID
					p.TransferEligible = true
					p.Status = domain.PaxCheckedIn
					t := ib.ScheduledAt - 90
					p.CheckedInAt = &t
					p.Itinerary = []string{ib.ID, f.ID}
				}
			}
			if p.InboundFlightID == "" {
				checkin := f.ScheduledAt - (65 + r.Float64()*80)
				p.CheckedInAt = &checkin
				p.Status = domain.PaxBooked
			}

			x := r.Float64()
			nBags := 0
			switch {
			case x < 0.08:
				nBags = 0
			case x < 0.58:
				nBags = 1
			case x < 0.86:
				nBags = 2
			case x < 0.97:
				nBags = 3
			default:
				nBags = 4
			}
			for k := 0; k < nBags; k++ {
				bagSeq++
				tag := fmt.Sprintf("880%07d", bagSeq)
				b := &domain.Bag{
					Tag: tag, ShortTag: tag[len(tag)-6:], PassengerID: paxID, PassengerName: name, PNR: pnr,
					OriginalFlightID: f.ID, FlightID: f.ID, NextFlightID: f.ID, Destination: f.Destination,
					SorterLane: 1 + r.Intn(4), ExpectedDisposition: fmt.Sprintf("depart_on_%s", f.ID),
				}
				if p.InboundFlightID != "" {
					b.Transfer = true
					b.InboundFlightID = p.InboundFlightID
					b.Status = domain.BagTransferHold
				} else {
					b.Status = domain.BagPlanned
				}
				db.Bags[tag] = b
				p.BagIDs = append(p.BagIDs, tag)
			}
			db.Passengers[paxID] = p
		}
	}

	buildScenarios(db, plan, outbound, inbound)
	return db, plan
}

func randomPNR(r *rand.Rand, n int) string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}

func sortedPaxForFlight(db *store.Store, flightID string) []*domain.Passenger {
	out := []*domain.Passenger{}
	for _, p := range db.Passengers {
		if p.FlightID == flightID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func firstPaxWithBags(db *store.Store, flightID string, exact int) *domain.Passenger {
	for _, p := range sortedPaxForFlight(db, flightID) {
		if exact == 0 && len(p.BagIDs) > 0 {
			return p
		}
		if exact > 0 && len(p.BagIDs) == exact {
			return p
		}
	}
	return nil
}

func addScenario(db *store.Store, plan *Plan, id, title string, at float64, item DirectorItem) {
	if _, exists := db.Scenarios[id]; !exists {
		db.ScenarioOrder = append(db.ScenarioOrder, id)
		db.Scenarios[id] = &domain.ScenarioResult{ID: id, Title: title, Status: "scheduled", ScheduledAt: at}
	}
	item.ScenarioID = id
	if item.ID == "" {
		item.ID = fmt.Sprintf("%s-item-%d", id, len(plan.Items)+1)
	}
	if item.At == 0 {
		item.At = at
	}
	plan.Items = append(plan.Items, item)
}

func flightByIndex(outbound []*domain.Flight, idx int) *domain.Flight {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(outbound) {
		idx = len(outbound) - 1
	}
	return outbound[idx]
}

func findLaterToDestination(outbound []*domain.Flight, dest string, after float64) *domain.Flight {
	for _, f := range outbound {
		if f.Destination == dest && f.ScheduledAt >= after {
			return f
		}
	}
	for _, f := range outbound {
		if f.ScheduledAt >= after {
			return f
		}
	}
	return outbound[len(outbound)-1]
}

func buildScenarios(db *store.Store, plan *Plan, outbound, inbound []*domain.Flight) {
	// 1. Headline no-show: exact third container / bottom layer.
	hf := flightByIndex(outbound, 18)
	hp := firstPaxWithBags(db, hf.ID, 1)
	if hp != nil {
		hp.Special = true
		tag := hp.BagIDs[0]
		b := db.Bags[tag]
		if len(hf.Containers) >= 3 {
			b.ForceContainerID = hf.Containers[2]
			b.ForceLayer = 1
		}
		at := hf.FinalCallAt - 3
		addScenario(db, plan, "SC-HEADLINE-NOSHOW", fmt.Sprintf("重点：%s 旅客未登机，箱子在第三个集装箱底层", hf.Number), at, DirectorItem{
			At: at, Kind: "no_show", RefID: hp.ID, Text: "headline",
		})
	}

	// Additional no-show cases for statistically meaningful metrics.
	noShowIndexes := []int{30, 37, 44, 51, 58, 66, 74, 83, 92, 103, 114}
	for n, idx := range noShowIndexes {
		f := flightByIndex(outbound, idx)
		p := firstPaxWithBags(db, f.ID, 0)
		if p == nil {
			continue
		}
		p.Special = true
		at := f.FinalCallAt - 2 - float64(n%3)
		addScenario(db, plan, fmt.Sprintf("SC-NOSHOW-%02d", n+1), fmt.Sprintf("未登机旅客 %s / %s 行李定位卸载", p.Name, f.Number), at, DirectorItem{
			At: at, Kind: "no_show", RefID: p.ID,
		})
	}

	// 2. Gate and aircraft changes at different handling stages.
	gf := flightByIndex(outbound, 20)
	gAt := gf.ScheduledAt - 96
	addScenario(db, plan, "SC-GATE", fmt.Sprintf("%s 起飞前换登机口，未装车行李改出口", gf.Number), gAt, DirectorItem{
		At: gAt, Kind: "gate_change", RefID: gf.ID, NewGate: "B18", NewChute: "C31", Text: "机坪调度原因",
	})

	af := flightByIndex(outbound, 28)
	aAt := af.ScheduledAt - 86
	addScenario(db, plan, "SC-AIRCRAFT", fmt.Sprintf("%s 更换飞机，未装车改路由/已装车通知装卸", af.Number), aAt, DirectorItem{
		At: aAt, Kind: "aircraft_change", RefID: af.ID, NewReg: "B-5912", NewModel: "A330-300", NewChute: "C34", Text: "前序飞机故障",
	})

	lgf := flightByIndex(outbound, 42)
	lgAt := lgf.ScheduledAt - 39
	addScenario(db, plan, "SC-LATE-GATE", fmt.Sprintf("%s 临关舱前换口，检验已装/未装分流通知", lgf.Number), lgAt, DirectorItem{
		At: lgAt, Kind: "gate_change", RefID: lgf.ID, NewGate: "A01", NewChute: "C03", Text: "航班保障机位调整",
	})

	// 3. Cancellation while the bag is physically on the sorter.
	cf := flightByIndex(outbound, 12)
	cp := firstPaxWithBags(db, cf.ID, 0)
	if cp != nil {
		cp.Special = true
		at := cf.ScheduledAt - 73
		addScenario(db, plan, "SC-CANCEL", fmt.Sprintf("旅客 %s 临时取消，值机撤销作用到分拣线", cp.Name), at, DirectorItem{
			At: at, Kind: "cancel_pax", RefID: cp.ID, Text: "旅客临时取消行程",
		})
	}

	// 4. Voluntary rebooking; bags must follow.
	rf := flightByIndex(outbound, 25)
	rp := firstPaxWithBags(db, rf.ID, 0)
	if rp != nil {
		rp.Special = true
		nf := findLaterToDestination(outbound, rf.Destination, rf.ScheduledAt+95)
		at := rf.ScheduledAt - 78
		addScenario(db, plan, "SC-REBOOK", fmt.Sprintf("旅客 %s 临时改签，行李自动改挂 %s", rp.Name, nf.Number), at, DirectorItem{
			At: at, Kind: "rebook_pax", RefID: rp.ID, RefID2: nf.ID, Text: "旅客临时申请更早/后续航班",
		})
	}

	// 5. Transfer inbound delays.
	delayIndexes := []int{8, 17, 26, 35, 44, 53, 61, 69}
	for n, idx := range delayIndexes {
		if idx >= len(inbound) {
			continue
		}
		f := inbound[idx]
		delay := 58 + float64((n*17)%70)
		at := f.ScheduledAt - 75
		addScenario(db, plan, fmt.Sprintf("SC-TRANSFER-DELAY-%02d", n+1), fmt.Sprintf("中转航班 %s 延误 %.0f 分钟，系统重算衔接", f.Number, delay), at, DirectorItem{
			At: at, Kind: "delay_inbound", RefID: f.ID, Value: delay,
		})
	}

	// 6. Unknown tag / missing BSM scans.
	orphanAt0 := domain.DayStart + 35
	addScenario(db, plan, "SC-ORPHAN", "扫到标签却找不到报文：异常出口与值机核实", orphanAt0, DirectorItem{
		At: orphanAt0, Kind: "orphan_scan", RefID: "7770000001",
	})
	for n := 1; n < 12; n++ {
		tag := fmt.Sprintf("777%07d", n+1)
		scanAt := 340 + float64(n)*47
		addScenario(db, plan, "SC-ORPHAN", "扫到标签却找不到报文：异常出口与值机核实", scanAt, DirectorItem{
			At: scanAt, Kind: "orphan_scan", RefID: tag,
		})
		if n < 9 {
			resolveAt := scanAt + 3.2
			target := flightByIndex(outbound, 5+n*9)
			addScenario(db, plan, "SC-ORPHAN", "扫到标签却找不到报文：异常出口与值机核实", resolveAt, DirectorItem{
				At: resolveAt, Kind: "resolve_orphan", RefID: tag, RefID2: target.ID,
			})
		}
	}
	// Resolve the first orphan a few minutes later.
	target := flightByIndex(outbound, 8)
	addScenario(db, plan, "SC-ORPHAN", "扫到标签却找不到报文：异常出口与值机核实", orphanAt0+4, DirectorItem{
		At: orphanAt0 + 4, Kind: "resolve_orphan", RefID: "7770000001", RefID2: target.ID,
	})

	// 7. Planted door-close problems. Reconciliation must catch every one.
	problemKinds := []string{"left_onboard", "misload", "missing_boarded_bag", "left_onboard", "misload", "missing_boarded_bag"}
	problemIndexes := []int{55, 64, 73, 82, 91, 101}
	for n, idx := range problemIndexes {
		f := flightByIndex(outbound, idx)
		kind := problemKinds[n]
		db.PlantedFaults[f.ID] = kind
		f.PlantedProblem = kind
		at := f.ReconcileAt - 3.2
		title := map[string]string{
			"left_onboard":        "人为/装卸差错：未登机旅客行李漏卸",
			"misload":             "人为/装卸差错：行李错装其他航班",
			"missing_boarded_bag": "人为/装卸差错：已登机旅客行李下落不一致",
		}[kind]
		addScenario(db, plan, fmt.Sprintf("SC-PROBLEM-%02d", n+1), fmt.Sprintf("关舱核对演练 %d：%s / %s", n+1, f.Number, title), at, DirectorItem{
			At: at, Kind: "inject_problem", RefID: f.ID, Text: kind,
		})
	}
}
