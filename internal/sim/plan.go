package sim

// DirectorItem is one scripted disruption used to prove the middleware rules.
type DirectorItem struct {
	ID         string
	ScenarioID string
	At         float64
	Kind       string
	RefID      string
	RefID2     string
	Value      float64
	Text       string
	NewGate    string
	NewChute   string
	NewReg     string
	NewModel   string
}

type Plan struct {
	Items []DirectorItem
}
