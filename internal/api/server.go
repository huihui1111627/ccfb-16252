package api

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"

	"airport-baggage/internal/domain"
	"airport-baggage/internal/metrics"
	"airport-baggage/internal/sim"
)

//go:embed web
var webFS embed.FS

type Server struct {
	engine *sim.Engine
	mux    *http.ServeMux
}

func NewServer(engine *sim.Engine) *Server {
	s := &Server{engine: engine, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/overview", s.handleOverview)
	s.mux.HandleFunc("GET /api/flights/{id}", s.handleFlightDetail)
	s.mux.HandleFunc("GET /api/bags/{tag}", s.handleBag)
	s.mux.HandleFunc("POST /api/control", s.handleControl)
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(static))
	s.mux.Handle("GET /", fileServer)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	now, running, speed := s.engine.Runtime()
	db := s.engine.DB()
	stats := metrics.Compute(db)

	db.Mu.RLock()
	defer db.Mu.RUnlock()

	flights := db.FlightList()
	chutes := db.ChuteList()

	// Refresh chute assignment metadata from the currently assigned flight.
	for _, c := range chutes {
		if c.Kind != "flight" {
			continue
		}
		c.FlightID = ""
		c.FlightNumber = ""
		c.Destination = ""
		c.Gate = ""
		for _, f := range flights {
			if f.Kind == "outbound" && f.ChuteID == c.ID {
				// Time-shared chutes are normal; show the flight in the active window.
				if now >= f.BoardingAt-95 && now <= f.DepartAt+20 {
					c.FlightID = f.ID
					c.FlightNumber = f.Number
					c.Destination = f.Destination
					c.Gate = f.Gate
					if f.ReleaseBlocked {
						c.Status = "exception"
					} else if now >= f.BoardingAt-50 {
						c.Status = "busy"
					} else {
						c.Status = "normal"
					}
					break
				}
			}
		}
	}

	sorter := []domain.SorterBagView{}
	flightBagCounts := map[string]int{}
	chuteActiveCounts := map[string]int{}
	for tag, b := range db.Bags {
		flightBagCounts[b.FlightID]++
		if b.ChuteID != "" && b.Status != domain.BagDeparted && b.Status != domain.BagVoided {
			chuteActiveCounts[b.ChuteID]++
		}
		if len(sorter) >= 110 {
			continue
		}
		active := b.Status == domain.BagInducted || b.Status == domain.BagScanning ||
			b.Status == domain.BagScanned || b.Status == domain.BagChuteAssigned ||
			b.Status == domain.BagMissingMessage || b.Status == domain.BagRecallChute
		if !active {
			continue
		}
		f := db.Flights[b.FlightID]
		flightNo := ""
		if f != nil {
			flightNo = f.Number
		}
		color := "blue"
		switch {
		case b.Status == domain.BagMissingMessage:
			color = "red"
		case b.Status == domain.BagRecallChute || b.Status == domain.BagVoided:
			color = "amber"
		case b.Transfer:
			color = "violet"
		case b.Status == domain.BagChuteAssigned:
			color = "green"
		}
		progress := b.Progress
		if progress <= 0 {
			progress = 0.2
		}
		if progress > 1 {
			progress = 1
		}
		sorter = append(sorter, domain.SorterBagView{
			Tag: tag, ShortTag: b.ShortTag, Color: color, Progress: progress,
			State: string(b.Status), ChuteID: b.ChuteID, FlightNumber: flightNo, Transfer: b.Transfer,
		})
	}
	sort.Slice(sorter, func(i, j int) bool { return sorter[i].Tag < sorter[j].Tag })
	for _, c := range chutes {
		c.ActiveBagCount = chuteActiveCounts[c.ID]
	}

	alerts := []*domain.Alert{}
	start := len(db.Alerts) - 34
	if start < 0 {
		start = 0
	}
	for i := len(db.Alerts) - 1; i >= start; i-- {
		alerts = append(alerts, db.Alerts[i])
	}

	events := []*domain.Message{}
	msgStart := len(db.Messages) - 42
	if msgStart < 0 {
		msgStart = 0
	}
	for i := len(db.Messages) - 1; i >= msgStart; i-- {
		events = append(events, db.Messages[i])
	}

	scenarios := db.ScenarioList()
	progress := (now - domain.DayStart) / (domain.DayEnd - domain.DayStart)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	writeJSON(w, http.StatusOK, domain.Overview{
		Now: now, Clock: domain.ClockString(now), DayProgress: progress,
		Running: running, Speed: speed, TotalSimDays: 1,
		Stats: stats, Flights: flights, Chutes: chutes, SorterBags: sorter,
		Alerts: alerts, Events: events, Scenarios: scenarios,
		FlightBagCounts: flightBagCounts, ChuteActiveCounts: chuteActiveCounts,
	})
}

func (s *Server) handleFlightDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	db := s.engine.DB()
	db.Mu.RLock()
	defer db.Mu.RUnlock()
	f := db.Flights[id]
	if f == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "flight not found"})
		return
	}
	var rec *domain.Reconciliation
	if len(f.Reconciliations) > 0 {
		r := f.Reconciliations[len(f.Reconciliations)-1]
		rec = &r
	}
	detail := domain.FlightDetail{
		Flight: f, Passengers: db.PassengersForFlight(id), Bags: db.BagsForFlight(id),
		Containers: db.ContainerListForFlight(id), Chute: db.Chutes[f.ChuteID], Reconciliation: rec,
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleBag(w http.ResponseWriter, r *http.Request) {
	tag := r.PathValue("tag")
	db := s.engine.DB()
	db.Mu.RLock()
	defer db.Mu.RUnlock()
	b := db.Bags[tag]
	if b == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bag not found"})
		return
	}
	writeJSON(w, http.StatusOK, b)
}

type controlRequest struct {
	Action string  `json:"action"`
	Speed  float64 `json:"speed"`
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	var req controlRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	switch req.Action {
	case "pause":
		s.engine.SetRunning(false)
	case "play":
		s.engine.SetRunning(true)
	case "toggle":
		s.engine.Toggle()
	case "reset":
		s.engine.Reset()
	case "finish":
		s.engine.FinishDay()
	case "speed":
		s.engine.SetSpeed(req.Speed)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})
		return
	}
	now, running, speed := s.engine.Runtime()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "now": now, "clock": domain.ClockString(now), "running": running, "speed": speed})
}
