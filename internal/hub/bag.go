package hub

import (
	"fmt"
	"hash/fnv"
	"math"

	"airport-baggage/internal/domain"
)

func hashInt(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() & 0x7fffffff)
}

func (s *Service) CheckIn(paxID string, at domain.SimMinute) bool {
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	if p == nil || p.Status != domain.PaxBooked {
		s.DB.Mu.Unlock()
		return false
	}
	p.Status = domain.PaxCheckedIn
	p.CheckedInAt = &at
	bagIDs := append([]string{}, p.BagIDs...)
	msgs := make([]*domain.Message, 0, len(bagIDs))
	for _, tag := range bagIDs {
		b := s.DB.Bags[tag]
		if b == nil {
			continue
		}
		b.Status = domain.BagBSMPublished
		b.BSMReceived = true
		b.ReadyAt = at + 2 + float64(hashInt(tag)%9)
		b.MessageID = fmt.Sprintf("BSM-%s", tag)
		AddBagEvent(b, at, domain.EventBSM, fmt.Sprintf("值机生成行李报文 %s：%s / %s", b.MessageID, b.FlightID, b.Destination), "值机系统")
		s.DB.NextMessageSeq++
		msgs = append(msgs, &domain.Message{
			ID:    fmt.Sprintf("MSG-%06d", s.DB.NextMessageSeq),
			At:    at,
			Topic: TopicCheckIn,
			Type:  "BSM_PUBLISHED",
			Key:   tag,
			Payload: map[string]interface{}{
				"tag": tag, "passenger": p.Name, "flightId": b.FlightID,
				"nextFlightId": b.NextFlightID, "destination": b.Destination,
			},
		})
	}
	s.DB.Mu.Unlock()
	for _, msg := range msgs {
		s.finishRecord(msg)
	}
	return true
}

// finishRecord stores a message whose sequence/ID was allocated while a
// caller held the store lock, then fans it out after unlock.
func (s *Service) finishRecord(msg *domain.Message) {
	s.DB.Mu.Lock()
	s.DB.Messages = append(s.DB.Messages, msg)
	s.DB.TotalMessages++
	s.DB.Mu.Unlock()
	s.Bus.Publish(msg)
}

func (s *Service) InductBag(tag string, at domain.SimMinute) bool {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil || b.Status != domain.BagBSMPublished || at < b.ReadyAt {
		s.DB.Mu.Unlock()
		return false
	}
	b.Status = domain.BagInducted
	t := at
	b.InductedAt = &t
	b.Progress = 0.08
	b.ReadyAt = at + 1.1 + float64(hashInt(tag+"scan")%9)/10.0
	AddBagEvent(b, at, domain.EventInduct, "行李进入分拣线，前往自动扫码段", "值机收运口")
	msg := s.lockedMessage(at, TopicSorter, "BAG_INDUCTED", tag, map[string]interface{}{"tag": tag, "lane": b.SorterLane})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	return true
}

func (s *Service) ScanBag(tag string, at domain.SimMinute) bool {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil {
		s.DB.Mu.Unlock()
		return false
	}
	if b.Status != domain.BagInducted && b.Status != domain.BagScanning {
		s.DB.Mu.Unlock()
		return false
	}
	if at < b.ReadyAt {
		s.DB.Mu.Unlock()
		return false
	}
	t := at
	b.ScannedAt = &t
	b.Progress = 0.48

	if !b.BSMReceived {
		b.Status = domain.BagMissingMessage
		b.UnknownScan = true
		b.ChuteID = "EXC"
		b.ReadyAt = 0
		AddBagEvent(b, at, domain.EventMissingMessage, "扫描到标签，但中间件找不到有效行李报文", "扫码器")
		msg := s.lockedMessage(at, TopicSorter, "BSM_NOT_FOUND", tag, map[string]interface{}{"tag": tag, "action": "divert_EXC"})
		s.DB.Mu.Unlock()
		s.finishRecord(msg)
		s.AddAlert(at, "critical", TopicSorter, "有签无报文：已送异常出口", fmt.Sprintf("标签 %s 扫描成功但查无 BSM，已分流至 7 号异常出口，请值机核实", tag), "bag", tag)
		return true
	}

	f := s.DB.Flights[b.FlightID]
	chute := ""
	flightNo := b.FlightID
	if f != nil {
		chute = f.ChuteID
		flightNo = f.Number
	}
	b.Status = domain.BagChuteAssigned
	b.ChuteID = chute
	if b.ExpectedChuteID == "" {
		b.ExpectedChuteID = chute
	}
	b.Progress = 0.78
	b.ReadyAt = at + 0.7 + float64(hashInt(tag+"chute")%8)/10.0
	AddBagEvent(b, at, domain.EventScan, fmt.Sprintf("条码/RFID 识别成功，报文决定前往 %s 出口（%s）", chute, flightNo), "扫码器")
	AddBagEvent(b, at, domain.EventChute, fmt.Sprintf("分拣机分配出口 %s", chute), "分拣机")
	msg := s.lockedMessage(at, TopicSorter, "CHUTE_ASSIGNED", tag, map[string]interface{}{"tag": tag, "chuteId": chute, "flightId": b.FlightID})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	return true
}

func (s *Service) ArriveChute(tag string, at domain.SimMinute) bool {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil || b.Status != domain.BagChuteAssigned || at < b.ReadyAt {
		s.DB.Mu.Unlock()
		return false
	}
	b.Status = domain.BagAtChute
	b.Progress = 1
	b.ReadyAt = 0
	if c := s.DB.Chutes[b.ChuteID]; c != nil {
		c.BagCount++
		c.RecentTags = append([]string{tag}, c.RecentTags...)
		if len(c.RecentTags) > 8 {
			c.RecentTags = c.RecentTags[:8]
		}
		c.FlashAt = at
	}
	AddBagEvent(b, at, "chute_arrival", fmt.Sprintf("到达 %s 出口，等待装卸扫描装车", b.ChuteID), b.ChuteID)
	s.DB.Mu.Unlock()
	return true
}

// HandleUnknownPhysicalScan models a bag tag seen by the scanner for which no
// BSM can be found at all. The tag is created as an exception aggregate so the
// operations UI can track verification and later resolution.
func (s *Service) HandleUnknownPhysicalScan(tag string, at domain.SimMinute) {
	s.DB.Mu.Lock()
	if _, exists := s.DB.Bags[tag]; exists {
		s.DB.Mu.Unlock()
		return
	}
	b := &domain.Bag{
		Tag: tag, ShortTag: shortTag(tag), Status: domain.BagMissingMessage,
		UnknownScan: true, ChuteID: "EXC", Progress: 0.62, SorterLane: 1 + hashInt(tag)%4,
		Reason:  "BSM lookup returned not found",
		History: []domain.BagEvent{{At: at, Type: domain.EventMissingMessage, Message: "物理标签被扫到，但值机/中间件无此报文", Location: "扫码器"}},
	}
	s.DB.Bags[tag] = b
	msg := s.lockedMessage(at, TopicSorter, "UNKNOWN_TAG_SCAN", tag, map[string]interface{}{"tag": tag, "action": "EXC"})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.AddAlert(at, "critical", TopicCheckIn, "扫到标签但找不到报文", fmt.Sprintf("未知标签 %s 已单独送至异常出口 EXC，请值机核实", tag), "bag", tag)
}

func (s *Service) ResolveUnknownScan(tag string, at domain.SimMinute, flightID, paxName string) {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil {
		s.DB.Mu.Unlock()
		return
	}
	b.UnknownResolved = true
	b.BSMReceived = true
	b.FlightID = flightID
	b.OriginalFlightID = flightID
	b.PassengerName = paxName
	paxID := b.PassengerID
	if paxID == "" {
		paxID = "P-VERIFY-" + tag
		b.PassengerID = paxID
		if _, exists := s.DB.Passengers[paxID]; !exists {
			boardedAt := at
			s.DB.Passengers[paxID] = &domain.Passenger{
				ID: paxID, PNR: "VFY" + tag[len(tag)-3:], Name: paxName,
				FlightID: flightID, OriginalFlightID: flightID, Itinerary: []string{flightID},
				Status: domain.PaxBoarded, CheckedInAt: &at, BoardedAt: &boardedAt,
				BagIDs: []string{tag},
			}
		}
	}
	b.Status = domain.BagChuteAssigned
	if f := s.DB.Flights[flightID]; f != nil {
		b.ChuteID = f.ChuteID
		b.Destination = f.Destination
	}
	b.Progress = 0.82
	b.ReadyAt = at + 0.5
	b.Correct = nil
	b.ExpectedDisposition = fmt.Sprintf("depart_on_%s", flightID)
	AddBagEvent(b, at, domain.EventCheckIn, "值机核实并补发电文，中间件重新路由到航班出口", "异常处理台")
	msg := s.lockedMessage(at, TopicCheckIn, "BSM_RECOVERED", tag, map[string]interface{}{"tag": tag, "flightId": flightID})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	s.AddAlert(at, "success", TopicCheckIn, "异常标签已核实并恢复路由", fmt.Sprintf("标签 %s 已补报并送往 %s", tag, b.ChuteID), "bag", tag)
}

func shortTag(tag string) string {
	if len(tag) >= 6 {
		return tag[len(tag)-6:]
	}
	return tag
}

func (s *Service) lockedMessage(at domain.SimMinute, topic, typ, key string, payload map[string]interface{}) *domain.Message {
	s.DB.NextMessageSeq++
	return &domain.Message{
		ID:      fmt.Sprintf("MSG-%06d", s.DB.NextMessageSeq),
		At:      at,
		Topic:   topic,
		Type:    typ,
		Key:     key,
		Payload: payload,
	}
}

func allocateContainerLocked(s *Service, b *domain.Bag) *domain.Container {
	if b.ForceContainerID != "" {
		if c := s.DB.Containers[b.ForceContainerID]; c != nil {
			return c
		}
	}
	fids := []string{}
	if f := s.DB.Flights[b.FlightID]; f != nil {
		fids = f.Containers
	}
	var chosen *domain.Container
	for _, cid := range fids {
		c := s.DB.Containers[cid]
		if c == nil || c.BagCount >= c.Capacity {
			continue
		}
		if chosen == nil || c.BagCount < chosen.BagCount {
			chosen = c
		}
	}
	if chosen == nil {
		// Dynamic overflow bulk container.
		id := fmt.Sprintf("BULK-%s-OVF", b.FlightID)
		chosen = s.DB.Containers[id]
		if chosen == nil {
			chosen = &domain.Container{ID: id, Code: "BULK-OVF", FlightID: b.FlightID, Kind: "bulk", Index: 99, Capacity: 120}
			s.DB.Containers[id] = chosen
			if f := s.DB.Flights[b.FlightID]; f != nil {
				f.Containers = append(f.Containers, id)
			}
		}
	}
	return chosen
}

func layerForContainer(c *domain.Container, forced int) int {
	if forced >= 1 && forced <= 3 {
		per := int(math.Ceil(float64(c.Capacity) / 3.0))
		if c.LayerCounts[forced-1] < per {
			return forced
		}
	}
	per := int(math.Ceil(float64(c.Capacity) / 3.0))
	for layer := 1; layer <= 3; layer++ {
		if c.LayerCounts[layer-1] < per {
			return layer
		}
	}
	return 3
}

func (s *Service) LoadBag(tag string, at domain.SimMinute) bool {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil {
		s.DB.Mu.Unlock()
		return false
	}
	if b.Status != domain.BagAtChute && b.Status != domain.BagChuteAssigned && b.Status != domain.BagRecovered {
		s.DB.Mu.Unlock()
		return false
	}
	c := allocateContainerLocked(s, b)
	if c == nil {
		s.DB.Mu.Unlock()
		return false
	}
	layer := layerForContainer(c, b.ForceLayer)
	c.LayerCounts[layer-1]++
	c.BagCount++
	c.BagTags = append(c.BagTags, tag)
	slotInLayer := c.LayerCounts[layer-1]
	slot := fmt.Sprintf("%s-%s-%02d", c.Code, []string{"L", "M", "T"}[layer-1], slotInLayer)
	t := at
	b.Status = domain.BagLoaded
	b.ContainerID = c.ID
	b.ContainerCode = c.Code
	b.Layer = layer
	b.LayerLabel = domain.LayerLabel(layer)
	b.Slot = slot
	b.LoadedAt = &t
	b.ReadyAt = 0
	b.Progress = 1
	b.ForceContainerID = ""
	b.ForceLayer = 0
	b.ForceSlot = ""
	AddBagEvent(b, at, domain.EventLoad, fmt.Sprintf("装卸扫描装车：%s 第%d层（%s），货位 %s", c.Code, layer, b.LayerLabel, slot), c.Code)
	msg := s.lockedMessage(at, TopicLoading, "BAG_LOADED", tag, map[string]interface{}{
		"tag": tag, "flightId": b.FlightID, "container": c.Code,
		"layer": layer, "slot": slot,
	})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	return true
}

func removeBagFromContainerLocked(s *Service, b *domain.Bag) {
	if b.ContainerID == "" {
		return
	}
	c := s.DB.Containers[b.ContainerID]
	if c == nil {
		return
	}
	for i, tag := range c.BagTags {
		if tag == b.Tag {
			c.BagTags = append(c.BagTags[:i], c.BagTags[i+1:]...)
			break
		}
	}
	if b.Layer >= 1 && b.Layer <= 3 && c.LayerCounts[b.Layer-1] > 0 {
		c.LayerCounts[b.Layer-1]--
	}
	if c.BagCount > 0 {
		c.BagCount--
	}
}

func (s *Service) OffloadBag(tag string, at domain.SimMinute, reason string) bool {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil {
		s.DB.Mu.Unlock()
		return false
	}
	wasLoaded := b.Status == domain.BagLoaded || b.Status == domain.BagMisloaded
	if wasLoaded {
		removeBagFromContainerLocked(s, b)
	}
	b.Status = domain.BagOffloaded
	b.Reason = reason
	t := at
	b.OffloadedAt = &t
	b.ReadyAt = 0
	AddBagEvent(b, at, domain.EventOffload, reason, b.ContainerCode)
	msg := s.lockedMessage(at, TopicLoading, "BAG_OFFLOADED", tag, map[string]interface{}{"tag": tag, "reason": reason, "wasLoaded": wasLoaded})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
	return true
}

func (s *Service) CompleteRecall(tag string, at domain.SimMinute) {
	s.DB.Mu.Lock()
	b := s.DB.Bags[tag]
	if b == nil || b.Status != domain.BagRecallChute {
		s.DB.Mu.Unlock()
		return
	}
	b.Status = domain.BagVoided
	t := at
	b.VoidedAt = &t
	b.ReadyAt = 0
	AddBagEvent(b, at, domain.EventVoid, "行李在召回/撤销出口完成拦截，不会装上飞机", "RCL")
	msg := s.lockedMessage(at, TopicBaggage, "BAG_VOIDED", tag, map[string]interface{}{"tag": tag})
	s.DB.Mu.Unlock()
	s.finishRecord(msg)
}
