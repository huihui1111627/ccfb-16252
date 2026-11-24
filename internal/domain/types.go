package domain

import (
	"fmt"
	"sort"
)

// SimMinute is simulated airport time in minutes after 00:00.
type SimMinute = float64

const DayStart SimMinute = 5 * 60
const DayEnd SimMinute = 24*60 + 30

// Bag states form the baggage lifecycle. New middleware commands append
// transitions to Bag.History instead of overwriting the previous location.
type BagState string

const (
	BagPlanned        BagState = "planned"
	BagBSMPublished   BagState = "bsm_published"
	BagAwaitInduct    BagState = "await_induct"
	BagInducted       BagState = "inducted"
	BagScanning       BagState = "scanning"
	BagMissingMessage BagState = "missing_message"
	BagScanned        BagState = "scanned"
	BagChuteAssigned  BagState = "chute_assigned"
	BagAtChute        BagState = "at_chute"
	BagExceptionChute BagState = "exception_chute"
	BagRecallChute    BagState = "recall_chute"
	BagLoaded         BagState = "loaded_container"
	BagMisloaded      BagState = "misloaded"
	BagOffloaded      BagState = "offloaded"
	BagRecovered      BagState = "recovered"
	BagDeparted       BagState = "departed"
	BagVoided         BagState = "voided"
	BagDelivered      BagState = "delivered"
	BagTransferHold   BagState = "transfer_hold"
	BagFailed         BagState = "failed"
)

type FlightStatus string

const (
	FlightScheduled FlightStatus = "scheduled"
	FlightBoarding  FlightStatus = "boarding"
	FlightFinalCall FlightStatus = "final_call"
	FlightBlocked   FlightStatus = "reconcile_blocked"
	FlightClosing   FlightStatus = "closing"
	FlightDeparted  FlightStatus = "departed"
	FlightDelayed   FlightStatus = "delayed"
	FlightCancelled FlightStatus = "cancelled"
)

type PaxStatus string

const (
	PaxBooked      PaxStatus = "booked"
	PaxCheckedIn   PaxStatus = "checked_in"
	PaxBoarded     PaxStatus = "boarded"
	PaxNoShow      PaxStatus = "no_show"
	PaxCancelled   PaxStatus = "cancelled"
	PaxRebooked    PaxStatus = "rebooked"
	PaxTransferred PaxStatus = "transferred"
)

type Checkpoint string

const (
	EventCheckIn        = "check_in"
	EventBSM            = "bsm"
	EventInduct         = "induct"
	EventScan           = "scan"
	EventMissingMessage = "missing_message"
	EventChute          = "chute"
	EventException      = "exception"
	EventLoad           = "load"
	EventLocate         = "locate"
	EventRetrieve       = "retrieve"
	EventOffload        = "offload"
	EventGateChange     = "gate_change"
	EventAircraftChange = "aircraft_change"
	EventRebook         = "rebook"
	EventCancel         = "cancel"
	EventVoid           = "void"
	EventReconcile      = "reconcile"
	EventRelease        = "release"
	EventDepart         = "depart"
	EventTransfer       = "transfer"
	EventFault          = "fault_injected"
	EventRemediation    = "remediation"
	EventNotify         = "notify"
)

type BagEvent struct {
	At       SimMinute `json:"at"`
	Type     string    `json:"type"`
	Message  string    `json:"message"`
	Location string    `json:"location,omitempty"`
}

type Notification struct {
	At      SimMinute `json:"at"`
	Channel string    `json:"channel"`
	To      string    `json:"to"`
	Message string    `json:"message"`
}

type GateChange struct {
	At      SimMinute `json:"at"`
	OldGate string    `json:"oldGate"`
	NewGate string    `json:"newGate"`
	Reason  string    `json:"reason,omitempty"`
}

type AircraftChange struct {
	At       SimMinute `json:"at"`
	OldReg   string    `json:"oldReg"`
	NewReg   string    `json:"newReg"`
	OldModel string    `json:"oldModel"`
	NewModel string    `json:"newModel"`
	Reason   string    `json:"reason,omitempty"`
}

type Flight struct {
	ID              string           `json:"id"`
	Number          string           `json:"number"`
	Airline         string           `json:"airline"`
	Kind            string           `json:"kind"` // outbound | inbound
	Origin          string           `json:"origin"`
	Destination     string           `json:"destination"`
	International   bool             `json:"international"`
	Gate            string           `json:"gate"`
	ChuteID         string           `json:"chuteId"`
	Stand           string           `json:"stand"`
	AircraftReg     string           `json:"aircraftReg"`
	AircraftModel   string           `json:"aircraftModel"`
	ContainerPlan   int              `json:"containerPlan"`
	ScheduledAt     SimMinute        `json:"scheduledAt"`
	EstimatedAt     SimMinute        `json:"estimatedAt"`
	ActualAt        *SimMinute       `json:"actualAt,omitempty"`
	ArrivalAt       SimMinute        `json:"arrivalAt,omitempty"`
	Status          FlightStatus     `json:"status"`
	DelayMinutes    float64          `json:"delayMinutes"`
	GateHistory     []GateChange     `json:"gateHistory,omitempty"`
	AircraftHistory []AircraftChange `json:"aircraftHistory,omitempty"`
	BoardingAt      SimMinute        `json:"boardingAt"`
	FinalCallAt     SimMinute        `json:"finalCallAt"`
	ReconcileAt     SimMinute        `json:"reconcileAt"`
	DepartAt        SimMinute        `json:"departAt"`
	Released        bool             `json:"released"`
	ReleaseBlocked  bool             `json:"releaseBlocked"`
	Reconciliations []Reconciliation `json:"reconciliations,omitempty"`
	PlantedProblem  string           `json:"plantedProblem,omitempty"`
	ScenarioTags    []string         `json:"scenarioTags,omitempty"`
	Containers      []string         `json:"containers,omitempty"`
	Capacity        int              `json:"capacity"`
}

type Passenger struct {
	ID               string         `json:"id"`
	PNR              string         `json:"pnr"`
	Name             string         `json:"name"`
	Phone            string         `json:"phone"`
	Special          bool           `json:"special"`
	Itinerary        []string       `json:"itinerary"`
	InboundFlightID  string         `json:"inboundFlightId,omitempty"`
	FlightID         string         `json:"flightId"`
	OriginalFlightID string         `json:"originalFlightId"`
	Status           PaxStatus      `json:"status"`
	CheckedInAt      *SimMinute     `json:"checkedInAt,omitempty"`
	BoardedAt        *SimMinute     `json:"boardedAt,omitempty"`
	CancelledAt      *SimMinute     `json:"cancelledAt,omitempty"`
	BagIDs           []string       `json:"bagIds"`
	Notifications    []Notification `json:"notifications,omitempty"`
	RebookCount      int            `json:"rebookCount"`
	TransferEligible bool           `json:"transferEligible"`
}

type Retrieval struct {
	QueryAt              SimMinute  `json:"queryAt"`
	PredictedContainerID string     `json:"predictedContainerId"`
	PredictedCode        string     `json:"predictedCode"`
	PredictedLayer       int        `json:"predictedLayer"`
	PredictedSlot        string     `json:"predictedSlot"`
	ActualContainerID    string     `json:"actualContainerId,omitempty"`
	ActualLayer          int        `json:"actualLayer,omitempty"`
	ActualSlot           string     `json:"actualSlot,omitempty"`
	Accurate             bool       `json:"accurate"`
	Status               string     `json:"status"` // locating | walking | digging | retrieved
	StartedAt            SimMinute  `json:"startedAt"`
	CompletedAt          *SimMinute `json:"completedAt,omitempty"`
	ElapsedSeconds       float64    `json:"elapsedSeconds,omitempty"`
	DigitalLocateMS      float64    `json:"digitalLocateMs"`
	BlindBaselineSeconds float64    `json:"blindBaselineSeconds"`
}

type Bag struct {
	Tag                 string     `json:"tag"`
	ShortTag            string     `json:"shortTag"`
	PassengerID         string     `json:"passengerId"`
	PassengerName       string     `json:"passengerName"`
	PNR                 string     `json:"pnr"`
	InboundFlightID     string     `json:"inboundFlightId,omitempty"`
	OriginalFlightID    string     `json:"originalFlightId"`
	FlightID            string     `json:"flightId"`
	NextFlightID        string     `json:"nextFlightId,omitempty"`
	Destination         string     `json:"destination"`
	Transfer            bool       `json:"transfer"`
	TransferOutcome     string     `json:"transferOutcome,omitempty"` // original | rebooked | failed
	MCTMinutes          float64    `json:"mctMinutes,omitempty"`
	Status              BagState   `json:"status"`
	Reason              string     `json:"reason,omitempty"`
	ChuteID             string     `json:"chuteId,omitempty"`
	ExpectedChuteID     string     `json:"expectedChuteId,omitempty"`
	SorterLane          int        `json:"sorterLane"`
	Progress            float64    `json:"progress"`
	InductedAt          *SimMinute `json:"inductedAt,omitempty"`
	ScannedAt           *SimMinute `json:"scannedAt,omitempty"`
	ChuteAt             *SimMinute `json:"chuteAt,omitempty"`
	ReadyAt             SimMinute  `json:"readyAt,omitempty"`
	ContainerID         string     `json:"containerId,omitempty"`
	ContainerCode       string     `json:"containerCode,omitempty"`
	Layer               int        `json:"layer,omitempty"`
	LayerLabel          string     `json:"layerLabel,omitempty"`
	Slot                string     `json:"slot,omitempty"`
	LoadedAt            *SimMinute `json:"loadedAt,omitempty"`
	OffloadedAt         *SimMinute `json:"offloadedAt,omitempty"`
	DepartedAt          *SimMinute `json:"departedAt,omitempty"`
	VoidedAt            *SimMinute `json:"voidedAt,omitempty"`
	MessageID           string     `json:"messageId,omitempty"`
	BSMReceived         bool       `json:"bsmReceived"`
	UnknownScan         bool       `json:"unknownScan"`
	UnknownResolved     bool       `json:"unknownResolved"`
	PlantedFault        string     `json:"plantedFault,omitempty"`
	ForceContainerID    string     `json:"-"`
	ForceLayer          int        `json:"-"`
	ForceSlot           string     `json:"-"`
	ExpectedDisposition string     `json:"expectedDisposition"`
	Correct             *bool      `json:"correct,omitempty"`
	Retrieval           *Retrieval `json:"retrieval,omitempty"`
	History             []BagEvent `json:"history"`
}

type Container struct {
	ID          string   `json:"id"`
	Code        string   `json:"code"`
	FlightID    string   `json:"flightId"`
	Kind        string   `json:"kind"` // AKE | bulk
	Index       int      `json:"index"`
	Capacity    int      `json:"capacity"`
	BagCount    int      `json:"bagCount"`
	LayerCounts [3]int   `json:"layerCounts"`
	BagTags     []string `json:"bagTags,omitempty"`
}

type Chute struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"` // flight | exception | recall | transfer
	FlightID       string    `json:"flightId,omitempty"`
	FlightNumber   string    `json:"flightNumber,omitempty"`
	Destination    string    `json:"destination,omitempty"`
	Gate           string    `json:"gate,omitempty"`
	Status         string    `json:"status"` // normal | busy | reroute | exception
	BagCount       int       `json:"bagCount"`
	ActiveBagCount int       `json:"activeBagCount"`
	RecentTags     []string  `json:"recentTags,omitempty"`
	FlashAt        SimMinute `json:"flashAt,omitempty"`
}

type ReconcileItem struct {
	BagTag        string `json:"bagTag"`
	PassengerName string `json:"passengerName"`
	Type          string `json:"type"` // boarded_bag_missing | nonboarded_bag_onboard | wrong_flight
	Detail        string `json:"detail"`
	Expected      string `json:"expected"`
	Actual        string `json:"actual"`
	Remediation   string `json:"remediation,omitempty"`
}

type Reconciliation struct {
	ID             string          `json:"id"`
	FlightID       string          `json:"flightId"`
	At             SimMinute       `json:"at"`
	Passed         bool            `json:"passed"`
	BoardedPax     int             `json:"boardedPax"`
	BoardedBags    int             `json:"boardedBags"`
	OnboardBags    int             `json:"onboardBags"`
	Issues         []ReconcileItem `json:"issues,omitempty"`
	PlantedProblem string          `json:"plantedProblem,omitempty"`
	RemediatedAt   *SimMinute      `json:"remediatedAt,omitempty"`
	Note           string          `json:"note,omitempty"`
}

type Alert struct {
	ID         string     `json:"id"`
	At         SimMinute  `json:"at"`
	Level      string     `json:"level"` // info | warn | critical | success
	Topic      string     `json:"topic"`
	Title      string     `json:"title"`
	Message    string     `json:"message"`
	RefType    string     `json:"refType,omitempty"`
	RefID      string     `json:"refId,omitempty"`
	Resolved   bool       `json:"resolved"`
	ResolvedAt *SimMinute `json:"resolvedAt,omitempty"`
}

type Message struct {
	ID      string                 `json:"id"`
	At      SimMinute              `json:"at"`
	Topic   string                 `json:"topic"`
	Type    string                 `json:"type"`
	Key     string                 `json:"key,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

type ScenarioResult struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Status      string     `json:"status"` // scheduled | running | passed | failed
	ScheduledAt SimMinute  `json:"scheduledAt"`
	CompletedAt *SimMinute `json:"completedAt,omitempty"`
	Summary     string     `json:"summary"`
	Detail      string     `json:"detail,omitempty"`
}

type Metrics struct {
	TotalPassengers   int     `json:"totalPassengers"`
	CheckedPassengers int     `json:"checkedPassengers"`
	TotalBags         int     `json:"totalBags"`
	ActiveSorterBags  int     `json:"activeSorterBags"`
	ExceptionBags     int     `json:"exceptionBags"`
	LoadedBags        int     `json:"loadedBags"`
	DepartedBags      int     `json:"departedBags"`
	OffloadedBags     int     `json:"offloadedBags"`
	VoidedBags        int     `json:"voidedBags"`
	CorrectBags       int     `json:"correctBags"`
	IncorrectBags     int     `json:"incorrectBags"`
	PendingAuditBags  int     `json:"pendingAuditBags"`
	CorrectnessRate   float64 `json:"correctnessRate"`

	NoShowCases           int     `json:"noShowCases"`
	LocationAccurateCases int     `json:"locationAccurateCases"`
	LocationAccuracyRate  float64 `json:"locationAccuracyRate"`
	AvgLocateMS           float64 `json:"avgLocateMs"`
	AvgRetrievalSeconds   float64 `json:"avgRetrievalSeconds"`
	MaxRetrievalSeconds   float64 `json:"maxRetrievalSeconds"`
	BaselineSearchSeconds float64 `json:"baselineSearchSeconds"`
	TimeSavedRate         float64 `json:"timeSavedRate"`

	TransferBags         int     `json:"transferBags"`
	TransferMadeOriginal int     `json:"transferMadeOriginal"`
	TransferAutoRebooked int     `json:"transferAutoRebooked"`
	TransferFinalSuccess int     `json:"transferFinalSuccess"`
	TransferCancelled    int     `json:"transferCancelled"`
	TransferFailed       int     `json:"transferFailed"`
	OriginalConnectRate  float64 `json:"originalConnectRate"`
	FinalConnectRate     float64 `json:"finalConnectRate"`

	PlantedProblemFlights  int `json:"plantedProblemFlights"`
	DetectedProblemFlights int `json:"detectedProblemFlights"`
	MissedProblemFlights   int `json:"missedProblemFlights"`
	BlockedReleases        int `json:"blockedReleases"`
	ReconciledFlights      int `json:"reconciledFlights"`
	ReleasedFlights        int `json:"releasedFlights"`
	DepartedFlights        int `json:"departedFlights"`

	GateChanges         int `json:"gateChanges"`
	AircraftChanges     int `json:"aircraftChanges"`
	RebookedPassengers  int `json:"rebookedPassengers"`
	CancelledPassengers int `json:"cancelledPassengers"`
	OrphanScans         int `json:"orphanScans"`
	OrphanResolved      int `json:"orphanResolved"`
	MessagesPublished   int `json:"messagesPublished"`
}

type SorterBagView struct {
	Tag          string  `json:"tag"`
	ShortTag     string  `json:"shortTag"`
	Color        string  `json:"color"`
	Progress     float64 `json:"progress"`
	State        string  `json:"state"`
	ChuteID      string  `json:"chuteId"`
	FlightNumber string  `json:"flightNumber"`
	Transfer     bool    `json:"transfer"`
}

type Overview struct {
	Now               SimMinute         `json:"now"`
	Clock             string            `json:"clock"`
	DayProgress       float64           `json:"dayProgress"`
	Running           bool              `json:"running"`
	Speed             float64           `json:"speed"`
	TotalSimDays      int               `json:"totalSimDays"`
	Stats             Metrics           `json:"stats"`
	Flights           []*Flight         `json:"flights"`
	Chutes            []*Chute          `json:"chutes"`
	SorterBags        []SorterBagView   `json:"sorterBags"`
	Alerts            []*Alert          `json:"alerts"`
	Events            []*Message        `json:"events"`
	Scenarios         []*ScenarioResult `json:"scenarios"`
	RecentTags        []string          `json:"recentTags"`
	FlightBagCounts   map[string]int    `json:"flightBagCounts"`
	ChuteActiveCounts map[string]int    `json:"chuteActiveCounts"`
}

type FlightDetail struct {
	Flight         *Flight         `json:"flight"`
	Passengers     []*Passenger    `json:"passengers"`
	Bags           []*Bag          `json:"bags"`
	Containers     []*Container    `json:"containers"`
	Chute          *Chute          `json:"chute"`
	Reconciliation *Reconciliation `json:"reconciliation"`
}

func ClockString(t SimMinute) string {
	if t < 0 {
		t = 0
	}
	minutes := int(t + 0.5)
	return fmt.Sprintf("%02d:%02d", minutes/60%24, minutes%60)
}

func LayerLabel(layer int) string {
	switch layer {
	case 1:
		return "底层"
	case 2:
		return "中层"
	case 3:
		return "顶层"
	default:
		return "未分层"
	}
}

func SortedBagTags(bags map[string]*Bag) []string {
	out := make([]string, 0, len(bags))
	for tag := range bags {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}
