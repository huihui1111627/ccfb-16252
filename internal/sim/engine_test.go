package sim

import (
	"testing"

	"airport-baggage/internal/domain"
	"airport-baggage/internal/metrics"
)

func TestFullDaySimulation(t *testing.T) {
	e := NewEngine(16252)
	for e.Clock() < domain.DayEnd {
		e.Tick(2)
	}
	m := metrics.Compute(e.DB())
	t.Logf("bags=%d pax=%d active=%d loaded=%d departed=%d offloaded=%d void=%d correct=%d incorrect=%d pending=%d rate=%.2f",
		m.TotalBags, m.TotalPassengers, m.ActiveSorterBags, m.LoadedBags, m.DepartedBags, m.OffloadedBags, m.VoidedBags,
		m.CorrectBags, m.IncorrectBags, m.PendingAuditBags, m.CorrectnessRate)
	t.Logf("noshow cases=%d accurate=%d avgRetrieval=%.1fs max=%.1fs", m.NoShowCases, m.LocationAccurateCases, m.AvgRetrievalSeconds, m.MaxRetrievalSeconds)
	t.Logf("transfer total=%d original=%d rebook=%d final=%d failed=%d origRate=%.1f finalRate=%.1f", m.TransferBags, m.TransferMadeOriginal, m.TransferAutoRebooked, m.TransferFinalSuccess, m.TransferFailed, m.OriginalConnectRate, m.FinalConnectRate)
	t.Logf("planted=%d detected=%d missed=%d reconciled=%d released=%d departed=%d", m.PlantedProblemFlights, m.DetectedProblemFlights, m.MissedProblemFlights, m.ReconciledFlights, m.ReleasedFlights, m.DepartedFlights)
	{
		n := 0
		for tag, b := range e.DB().Bags {
			if b.Transfer && b.TransferOutcome == "" && n < 3 {
				t.Logf("BLANK %s status=%s inbound=%s flight=%s scanned=%v events=%d", tag, b.Status, b.InboundFlightID, b.FlightID, b.ScannedAt != nil, len(b.History))
				for _, ev := range b.History {
					t.Logf("  %.1f %s %s", ev.At, ev.Type, ev.Message)
				}
				n++
			}
		}
	}
	if m.MissedProblemFlights != 0 {
		t.Fatalf("missed planted problem flights: %d", m.MissedProblemFlights)
	}
	if m.IncorrectBags != 0 {
		statusCounts := map[string]int{}
		blankTransfer := map[string]int{}
		for _, b := range e.DB().Bags {
			statusCounts[string(b.Status)]++
			if b.Transfer && b.TransferOutcome == "" {
				blankTransfer[string(b.Status)]++
			}
		}
		t.Logf("statusCounts=%v blankTransfer=%v", statusCounts, blankTransfer)
		// Print a few incorrect tags for diagnosis.
		n := 0
		for tag, b := range e.DB().Bags {
			if b.Transfer && b.TransferOutcome == "" && n < 3 {
				t.Logf("BLANK %s status=%s inbound=%s flight=%s scanned=%v history:", tag, b.Status, b.InboundFlightID, b.FlightID, b.ScannedAt != nil)
				for _, ev := range b.History {
					t.Logf("  %.1f %s %s", ev.At, ev.Type, ev.Message)
				}
				n++
			}
		}
		n = 0
		for tag, b := range e.DB().Bags {
			if b.Correct != nil && !*b.Correct {
				t.Logf("INCORRECT %s status=%s flight=%s expected=%s pax=%s", tag, b.Status, b.FlightID, b.ExpectedDisposition, b.PassengerID)
				n++
				if n >= 10 {
					break
				}
			}
		}
		t.Fatalf("incorrect bags: %d pending: %d", m.IncorrectBags, m.PendingAuditBags)
	}
}
