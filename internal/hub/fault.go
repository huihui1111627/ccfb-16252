package hub

import (
	"sort"

	"airport-baggage/internal/domain"
)

// InjectDoorFault deliberately creates one of the handling defects that the
// door-close reconciliation is required to catch. It represents physical
// handling/chaos outside the happy-path middleware flow.
func (s *Service) InjectDoorFault(flightID, kind string, at domain.SimMinute) string {
	s.DB.Mu.Lock()
	f := s.DB.Flights[flightID]
	if f == nil {
		s.DB.Mu.Unlock()
		return ""
	}
	tags := []string{}
	for tag, b := range s.DB.Bags {
		if b.FlightID == flightID && b.Status == domain.BagLoaded && b.ContainerID != "" {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	if len(tags) == 0 {
		s.DB.Mu.Unlock()
		return ""
	}
	tag := tags[len(tags)/2]
	b := s.DB.Bags[tag]
	p := s.DB.Passengers[b.PassengerID]
	var msg *domain.Message

	switch kind {
	case "left_onboard":
		if p != nil {
			p.Status = domain.PaxNoShow
			p.BoardedAt = nil
		}
		AddBagEvent(b, at, domain.EventFault, "演练缺陷：装卸班组漏收卸载指令，行李仍留在机上集装箱", b.ContainerCode)
		msg = s.lockedMessage(at, TopicReconcile, "FAULT_LEFT_ONBOARD", tag, map[string]interface{}{"flightId": flightID})

	case "missing_boarded_bag":
		if p != nil {
			p.Status = domain.PaxBoarded
			t := at - 4
			p.BoardedAt = &t
		}
		removeBagFromContainerLocked(s, b)
		t := at
		b.Status = domain.BagOffloaded
		b.OffloadedAt = &t
		b.Reason = "演练缺陷：装车扫描后实物/舱单状态被错误移除"
		AddBagEvent(b, at, domain.EventFault, "演练缺陷：已登机旅客行李从装机清单/集装箱记录中消失", b.ContainerCode)
		msg = s.lockedMessage(at, TopicReconcile, "FAULT_MISSING_BOARDED_BAG", tag, map[string]interface{}{"flightId": flightID})

	case "misload":
		if p != nil {
			p.Status = domain.PaxBoarded
			t := at - 5
			p.BoardedAt = &t
		}
		removeBagFromContainerLocked(s, b)
		otherIDs := []string{}
		for id, of := range s.DB.Flights {
			if of.Kind == "outbound" && id != flightID && of.ScheduledAt >= f.ScheduledAt+35 && len(of.Containers) > 0 {
				otherIDs = append(otherIDs, id)
			}
		}
		sort.Strings(otherIDs)
		if len(otherIDs) == 0 {
			// Put it physically on a container of the same aircraft but keep
			// the scan state wrong; reconciliation still catches the mismatch.
			b.Status = domain.BagMisloaded
			AddBagEvent(b, at, domain.EventFault, "演练缺陷：行李被推入错误集装箱", b.ContainerCode)
		} else {
			oid := otherIDs[hashInt(tag)%len(otherIDs)]
			of := s.DB.Flights[oid]
			cid := of.Containers[hashInt(tag+"c")%len(of.Containers)]
			c := s.DB.Containers[cid]
			if c != nil {
				layer := 1 + hashInt(tag+"layer")%3
				c.LayerCounts[layer-1]++
				c.BagCount++
				c.BagTags = append(c.BagTags, tag)
				b.ContainerID = c.ID
				b.ContainerCode = c.Code
				b.Layer = layer
				b.LayerLabel = domain.LayerLabel(layer)
				b.Slot = "MISLOAD-" + tag[len(tag)-4:]
			}
			b.Status = domain.BagMisloaded
			AddBagEvent(b, at, domain.EventFault, "演练缺陷：行李被错装入 "+oid+" / "+b.ContainerCode, b.ContainerCode)
		}
		msg = s.lockedMessage(at, TopicReconcile, "FAULT_MISLOAD", tag, map[string]interface{}{"flightId": flightID})
	}
	s.DB.Mu.Unlock()
	if msg != nil {
		s.finishRecord(msg)
	}
	s.AddAlert(at, "critical", TopicReconcile, "已注入关舱核对演练缺陷", f.Number+" / "+kind+" / "+tag, "flight", flightID)
	return tag
}
