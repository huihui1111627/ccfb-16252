package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"airport-baggage/internal/domain"
	"airport-baggage/internal/metrics"
	"airport-baggage/internal/sim"
)

func main() {
	seed := flag.Int64("seed", 16252, "deterministic simulation seed")
	jsonOut := flag.Bool("json", false, "emit metrics as JSON")
	flag.Parse()

	e := sim.NewEngine(*seed)
	for e.Clock() < domain.DayEnd {
		e.Tick(2)
	}
	m := metrics.Compute(e.DB())

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(m)
	} else {
		fmt.Println("== 枢纽机场行李中间件 · 一整天确定性演练报告 ==")
		fmt.Printf("旅客 %d，行李报文/标签 %d，报文总量 %d\n", m.TotalPassengers, m.TotalBags, m.MessagesPublished)
		fmt.Printf("最终去向正确：%d/%d，正确率 %.2f%%（错误 %d，未审计 %d）\n", m.CorrectBags, m.TotalBags, m.CorrectnessRate, m.IncorrectBags, m.PendingAuditBags)
		fmt.Printf("未登机定位：%d 件，准确 %d，准确率 %.1f%%；平均取出 %.1f 秒，最大 %.1f 秒；盲找基线 %.0f 秒\n",
			m.NoShowCases, m.LocationAccurateCases, m.LocationAccuracyRate, m.AvgRetrievalSeconds, m.MaxRetrievalSeconds, m.BaselineSearchSeconds)
		fmt.Printf("中转行李：%d，原航班衔接 %d，自动保护改签 %d，最终成功 %d，取消 %d，失败 %d，最终成功率 %.1f%%\n",
			m.TransferBags, m.TransferMadeOriginal, m.TransferAutoRebooked, m.TransferFinalSuccess, m.TransferCancelled, m.TransferFailed, m.FinalConnectRate)
		fmt.Printf("关舱核对：植入问题 %d，发现 %d，漏检 %d；完成核对 %d 班，放行 %d 班，起飞 %d 班\n",
			m.PlantedProblemFlights, m.DetectedProblemFlights, m.MissedProblemFlights, m.ReconciledFlights, m.ReleasedFlights, m.DepartedFlights)
		fmt.Printf("异常标签：扫描 %d，核实恢复 %d；换口 %d 次，换机 %d 次，改签旅客 %d，取消旅客 %d\n",
			m.OrphanScans, m.OrphanResolved, m.GateChanges, m.AircraftChanges, m.RebookedPassengers, m.CancelledPassengers)
	}

	if m.IncorrectBags != 0 || m.MissedProblemFlights != 0 || m.TransferFailed != 0 {
		os.Exit(1)
	}
}
