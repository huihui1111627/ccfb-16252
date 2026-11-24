package hub

import (
	"fmt"

	"airport-baggage/internal/domain"
)

// AssessTransfer checks Minimum Connecting Time against the current estimated
// departure. If the original flight cannot be caught, the passenger and bag are
// protected onto the earliest feasible later flight.
func (s *Service) AssessTransfer(tag string, at domain.SimMinute) (outcome string, newFlightID string) {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil || !b.Transfer || b.TransferOutcome != "" {
		s.DB.Mu.Unlock()
		return "", ""
	}
	f := s.DB.Flights[b.FlightID]
	mct := 45.0
	if f != nil && f.International {
		mct = 75.0
	}
	b.MCTMinutes = mct
	wasHold := b.Status == domain.BagTransferHold
	dep := at
	if f != nil {
		dep = f.EstimatedAt
		if dep == 0 {
			dep = f.DepartAt
		}
	}
	if f != nil && dep-at >= mct {
		b.TransferOutcome = "original"
		AddBagEvent(b, at, domain.EventTransfer, fmt.Sprintf("到达后衔接剩余 %.0f 分钟，满足 MCT %.0f，按原航班 %s 分拣", dep-at, mct, f.Number), "中转分拣")
		msg := s.lockedMessage(at, TopicTransfer, "TRANSFER_FEASIBLE", tag, map[string]interface{}{"remaining": dep - at, "mct": mct})
		s.DB.Mu.Unlock()
		s.finishRecord(msg)
		return "original", b.FlightID
	}

	// Not feasible: find earliest same-destination outbound that can still be made.
	dest := b.Destination
	var candidate *domain.Flight
	for _, cf := range s.DB.Flights {
		if cf.Kind != "outbound" || cf.Destination != dest || cf.ID == b.FlightID {
			continue
		}
		if cf.DepartAt < at+mct+8 {
			continue
		}
		if candidate != nil && cf.DepartAt >= candidate.DepartAt {
			continue
		}
		paxCount := 0
		for _, p := range s.DB.Passengers {
			if p.FlightID == cf.ID && p.Status != domain.PaxCancelled {
				paxCount++
			}
		}
		if paxCount >= cf.Capacity {
			continue
		}
		candidate = cf
	}
	paxID := b.PassengerID
	oldFlight := b.FlightID
	destCopy := dest
	if candidate == nil {
		// The network control desk can always add a recovery section. This
		// keeps the closed-world simulation from stranding a transfer bag.
		s.DB.Mu.Unlock()
		candidate = s.CreateRecoveryFlight(destCopy, mct, at)
		s.DB.Mu.Lock()
	}
	newID := candidate.ID
	newNo := candidate.Number
	s.DB.Mu.Unlock()

	s.RebookPassenger(paxID, newID, at, "中转航班延误，最短衔接时间不足，系统自动保护改签")
	s.DB.Mu.Lock()
	if nb := s.DB.Bags[tag]; nb != nil {
		nb.Transfer = true
		nb.TransferOutcome = "rebooked"
		nb.MCTMinutes = mct
		if wasHold {
			// Proactive re-protection can happen before the inbound arrives.
			// Keep the bag in transfer hold until physical arrival.
			nb.Status = domain.BagTransferHold
			nb.ReadyAt = 0
			AddBagEvent(nb, at, domain.EventTransfer, "已提前保护改挂；行李随机到达后再进入中转分拣", nb.InboundFlightID)
		}
	}
	if p := s.DB.Passengers[paxID]; p != nil {
		// Transfer service owns the rebook notification wording.
		_ = p
	}
	msg := s.lockedMessage(at, TopicTransfer, "TRANSFER_AUTO_REBOOKED", tag, map[string]interface{}{
		"oldFlightId": oldFlight, "newFlightId": newID, "mct": mct,
	})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.NotifyPassenger(at, paxID, "中转短信/APP", fmt.Sprintf("因前段延误，已为您和行李自动改乘 %s；请按新登机口登机。", newNo))
	s.AddAlert(at, "warn", TopicTransfer, "中转来不及，已提前改下一班并通知旅客", fmt.Sprintf("行李 %s：%s → %s", tag, oldFlight, newID), "bag", tag)
	return "rebooked", newID
}

func (s *Service) AssessTransfersForInbound(inboundID string, at domain.SimMinute) int {
	tags := []string{}
	s.DB.Mu.RLock()
	for tag, b := range s.DB.Bags {
		if b.InboundFlightID == inboundID && b.Transfer && b.TransferOutcome == "" {
			tags = append(tags, tag)
		}
	}
	s.DB.Mu.RUnlock()
	n := 0
	for _, tag := range tags {
		if outcome, _ := s.AssessTransfer(tag, at); outcome != "" {
			n++
		}
	}
	return n
}

// CreateRecoveryFlight adds an ad-hoc protection flight when no scheduled
// same-destination flight has enough time/capacity.
func (s *Service) CreateRecoveryFlight(dest string, mct, at domain.SimMinute) *domain.Flight {
	s.DB.Mu.Lock()
	n := len(s.DB.Flights) + 1
	id := fmt.Sprintf("RCV%03d", n)
	chuteID := fmt.Sprintf("RCV-C%02d", n)
	dep := at + mct + 18
	f := &domain.Flight{
		ID: id, Number: fmt.Sprintf("RC%04d", 8000+n), Airline: "枢纽补班", Kind: "outbound",
		Origin: "中州枢纽", Destination: dest, Gate: fmt.Sprintf("R%02d", 1+n%8), ChuteID: chuteID,
		Stand: "RCV", AircraftReg: fmt.Sprintf("B-R%04d", n), AircraftModel: "A321neo",
		ContainerPlan: 8, ScheduledAt: dep, EstimatedAt: dep, Status: domain.FlightScheduled,
		Capacity: 220, BoardingAt: dep - 30, FinalCallAt: dep - 16, ReconcileAt: dep - 10, DepartAt: dep,
		ScenarioTags: []string{"recovery_flight"},
	}
	s.DB.Flights[id] = f
	s.DB.Chutes[chuteID] = &domain.Chute{ID: chuteID, Name: "中转补班专用出口", Kind: "flight", FlightID: id, FlightNumber: f.Number, Destination: dest, Gate: f.Gate, Status: "reroute"}
	for i := 0; i < 8; i++ {
		cid := fmt.Sprintf("%s-CTR-%02d", id, i+1)
		c := &domain.Container{ID: cid, Code: fmt.Sprintf("AKE%05dRC", 70000+n*10+i), FlightID: id, Kind: "AKE", Index: i + 1, Capacity: 48}
		s.DB.Containers[cid] = c
		f.Containers = append(f.Containers, cid)
	}
	msg := s.lockedMessage(at, TopicOperations, "RECOVERY_FLIGHT_CREATED", id, map[string]interface{}{"destination": dest, "departAt": dep})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.AddAlert(at, "info", TopicTransfer, "已追加中转保护补班", fmt.Sprintf("%s 前往 %s，起飞 %s", f.Number, dest, domain.ClockString(dep)), "flight", id)
	return f
}
