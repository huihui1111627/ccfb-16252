package store

import (
	"sort"
	"sync"

	"airport-baggage/internal/domain"
)

type Store struct {
	Mu sync.RWMutex

	Flights       map[string]*domain.Flight
	Passengers    map[string]*domain.Passenger
	Bags          map[string]*domain.Bag
	Containers    map[string]*domain.Container
	Chutes        map[string]*domain.Chute
	Messages      []*domain.Message
	Alerts        []*domain.Alert
	Scenarios     map[string]*domain.ScenarioResult
	ScenarioOrder []string

	// PlantedFaults maps flight ID to the chaos experiment that must be
	// caught by door-close reconciliation.
	PlantedFaults map[string]string

	NextMessageSeq int
	NextAlertSeq   int
	TotalMessages  int
}

func New() *Store {
	return &Store{
		Flights:       map[string]*domain.Flight{},
		Passengers:    map[string]*domain.Passenger{},
		Bags:          map[string]*domain.Bag{},
		Containers:    map[string]*domain.Container{},
		Chutes:        map[string]*domain.Chute{},
		Scenarios:     map[string]*domain.ScenarioResult{},
		PlantedFaults: map[string]string{},
	}
}

func (s *Store) FlightList() []*domain.Flight {
	out := make([]*domain.Flight, 0, len(s.Flights))
	for _, f := range s.Flights {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScheduledAt == out[j].ScheduledAt {
			return out[i].Number < out[j].Number
		}
		return out[i].ScheduledAt < out[j].ScheduledAt
	})
	return out
}

func (s *Store) ChuteList() []*domain.Chute {
	out := make([]*domain.Chute, 0, len(s.Chutes))
	for _, c := range s.Chutes {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) ContainerListForFlight(flightID string) []*domain.Container {
	out := []*domain.Container{}
	for _, c := range s.Containers {
		if c.FlightID == flightID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func (s *Store) BagsForFlight(flightID string) []*domain.Bag {
	out := []*domain.Bag{}
	for _, b := range s.Bags {
		if b.FlightID == flightID || b.OriginalFlightID == flightID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out
}

func (s *Store) PassengersForFlight(flightID string) []*domain.Passenger {
	out := []*domain.Passenger{}
	for _, p := range s.Passengers {
		if p.FlightID == flightID || p.OriginalFlightID == flightID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PNR < out[j].PNR })
	return out
}

func (s *Store) LatestReconciliation(flightID string) *domain.Reconciliation {
	f := s.Flights[flightID]
	if f == nil || len(f.Reconciliations) == 0 {
		return nil
	}
	r := f.Reconciliations[len(f.Reconciliations)-1]
	return &r
}

func (s *Store) ScenarioList() []*domain.ScenarioResult {
	out := make([]*domain.ScenarioResult, 0, len(s.ScenarioOrder))
	for _, id := range s.ScenarioOrder {
		if sc := s.Scenarios[id]; sc != nil {
			out = append(out, sc)
		}
	}
	return out
}
