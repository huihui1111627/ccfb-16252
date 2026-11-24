package hub

import (
	"fmt"

	"airport-baggage/internal/domain"
	"airport-baggage/internal/middleware"
	"airport-baggage/internal/store"
)

const (
	TopicCheckIn    = "checkin"
	TopicBaggage    = "baggage"
	TopicSorter     = "sorter"
	TopicLoading    = "loading"
	TopicDeparture  = "departure"
	TopicTransfer   = "transfer"
	TopicReconcile  = "reconcile"
	TopicOperations = "operations"
)

type Service struct {
	DB  *store.Store
	Bus *middleware.Bus
}

func NewService(db *store.Store, bus *middleware.Bus) *Service {
	svc := &Service{DB: db, Bus: bus}
	bus.Subscribe("*", func(msg *domain.Message) {
		// The bus is intentionally durable-looking through Service.Record;
		// this wildcard subscriber represents monitoring/audit consumers.
	})
	return svc
}

// RecordMessage appends a middleware envelope and fans it out to subscribers.
func (s *Service) RecordMessage(at domain.SimMinute, topic, typ, key string, payload map[string]interface{}) *domain.Message {
	s.DB.Mu.Lock()
	s.DB.NextMessageSeq++
	msg := &domain.Message{
		ID:      fmt.Sprintf("MSG-%06d", s.DB.NextMessageSeq),
		At:      at,
		Topic:   topic,
		Type:    typ,
		Key:     key,
		Payload: payload,
	}
	s.DB.Messages = append(s.DB.Messages, msg)
	s.DB.TotalMessages++
	s.DB.Mu.Unlock()
	s.Bus.Publish(msg)
	return msg
}

func (s *Service) AddAlert(at domain.SimMinute, level, topic, title, message, refType, refID string) *domain.Alert {
	s.DB.Mu.Lock()
	s.DB.NextAlertSeq++
	a := &domain.Alert{
		ID:      fmt.Sprintf("ALT-%04d", s.DB.NextAlertSeq),
		At:      at,
		Level:   level,
		Topic:   topic,
		Title:   title,
		Message: message,
		RefType: refType,
		RefID:   refID,
	}
	s.DB.Alerts = append(s.DB.Alerts, a)
	if len(s.DB.Alerts) > 500 {
		s.DB.Alerts = s.DB.Alerts[len(s.DB.Alerts)-500:]
	}
	s.DB.Mu.Unlock()
	s.RecordMessage(at, TopicOperations, "ALERT_"+level, a.ID, map[string]interface{}{
		"title":   title,
		"message": message,
		"refId":   refID,
	})
	return a
}

func (s *Service) ResolveAlertPrefix(at domain.SimMinute, refID string) {
	s.DB.Mu.Lock()
	for i := len(s.DB.Alerts) - 1; i >= 0; i-- {
		a := s.DB.Alerts[i]
		if !a.Resolved && (a.RefID == refID || (len(refID) > 3 && len(a.RefID) >= len(refID) && a.RefID[:len(refID)] == refID)) {
			a.Resolved = true
			t := at
			a.ResolvedAt = &t
		}
	}
	s.DB.Mu.Unlock()
}

func (s *Service) NotifyPassenger(at domain.SimMinute, paxID, channel, message string) {
	s.DB.Mu.Lock()
	p := s.DB.Passengers[paxID]
	if p != nil {
		p.Notifications = append(p.Notifications, domain.Notification{
			At: at, Channel: channel, To: p.Phone, Message: message,
		})
	}
	s.DB.Mu.Unlock()
	s.RecordMessage(at, TopicOperations, "PASSENGER_NOTIFY", paxID, map[string]interface{}{
		"passengerId": paxID,
		"channel":     channel,
		"message":     message,
	})
}

func AddBagEvent(b *domain.Bag, at domain.SimMinute, typ, msg, location string) {
	b.History = append(b.History, domain.BagEvent{At: at, Type: typ, Message: msg, Location: location})
	if len(b.History) > 30 {
		b.History = b.History[len(b.History)-30:]
	}
}

func (s *Service) SetScenario(id, status, summary, detail string, completedAt *domain.SimMinute) {
	s.DB.Mu.Lock()
	if sc := s.DB.Scenarios[id]; sc != nil {
		sc.Status = status
		if summary != "" {
			sc.Summary = summary
		}
		if detail != "" {
			sc.Detail = detail
		}
		if completedAt != nil {
			sc.CompletedAt = completedAt
		}
	}
	s.DB.Mu.Unlock()
}
