package capacity

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SummaryCell struct {
	CellID                        string  `json:"cell_id"`
	DatasetID                     string  `json:"dataset_id"`
	Identities                    int     `json:"identities"`
	Models                        int     `json:"models"`
	APIKeys                       int     `json:"api_keys"`
	ResourceID                    string  `json:"resource_id"`
	CPU                           float64 `json:"cpu"`
	MemoryMiB                     int     `json:"memory_mib"`
	Status                        string  `json:"status"`
	StartupSeconds                float64 `json:"startup_seconds"`
	WarmupSeconds                 float64 `json:"warmup_seconds"`
	HardEventsPerSecond           int     `json:"hard_events_per_second"`
	InteractiveEventsPerSecond    int     `json:"interactive_events_per_second"`
	RecommendedEventsPerSecond    int     `json:"recommended_events_per_second"`
	PeakMemoryBytes               int64   `json:"peak_memory_bytes"`
	CapacityPeakMemoryBytes       int64   `json:"capacity_peak_memory_bytes"`
	CapacityCPUUtilizationPercent float64 `json:"capacity_cpu_utilization_percent"`
	BoundaryHTTPP95MS             float64 `json:"capacity_http_core_p95_ms"`
	BoundaryHTTPP99MS             float64 `json:"capacity_http_overall_p99_ms"`
	DashboardStatus               string  `json:"dashboard_status"`
	SlowestDashboardPath          string  `json:"slowest_dashboard_path,omitempty"`
	SlowestDashboardP99MS         float64 `json:"slowest_dashboard_p99_ms"`
	SharedDriver                  bool    `json:"shared_driver"`
	Error                         string  `json:"error,omitempty"`
}

type HardwareRecommendation struct {
	DatasetID                   string  `json:"dataset_id"`
	ResourceID                  string  `json:"resource_id"`
	CPU                         float64 `json:"cpu"`
	MemoryMiB                   int     `json:"memory_mib"`
	IngestionMaxEventsPerSecond int     `json:"ingestion_max_events_per_second"`
	DashboardMaxEventsPerSecond int     `json:"dashboard_max_events_per_second"`
	RecommendedEventsPerSecond  int     `json:"recommended_events_per_second"`
	CPUUtilizationPercent       float64 `json:"capacity_cpu_utilization_percent"`
	CoreP95MS                   float64 `json:"capacity_http_core_p95_ms"`
	OverallP99MS                float64 `json:"capacity_http_overall_p99_ms"`
	PeakMemoryBytes             int64   `json:"peak_memory_bytes"`
	SharedDriver                bool    `json:"shared_driver"`
	Guidance                    string  `json:"guidance"`
}

type RunSummary struct {
	GeneratedAt time.Time                `json:"generated_at"`
	Cells       []SummaryCell            `json:"cells"`
	Hardware    []HardwareRecommendation `json:"hardware"`
	Failures    int                      `json:"failures"`
}

func SummarizeRun(runDir string) (RunSummary, error) {
	results, err := loadCellResults(runDir)
	if err != nil {
		return RunSummary{}, err
	}
	summary := BuildSummary(results)
	if err := WriteJSONAtomic(filepath.Join(runDir, "summary.json"), summary); err != nil {
		return RunSummary{}, err
	}
	if err := writeSummaryCSV(filepath.Join(runDir, "summary.csv"), summary); err != nil {
		return RunSummary{}, err
	}
	if err := writeReportMarkdown(filepath.Join(runDir, "report.md"), summary); err != nil {
		return RunSummary{}, err
	}
	return summary, nil
}

func BuildSummary(results []CellResult) RunSummary {
	summary := RunSummary{GeneratedAt: time.Now()}
	for _, result := range results {
		p95, p99, slowestPath, slowestP99 := capacityLatency(result.Attempts, result.Capacity.HardEventsPerSecond)
		cpuUtilization, capacityPeakMemory := capacityTelemetry(result.Attempts, result.Capacity.HardEventsPerSecond)
		cell := SummaryCell{
			CellID: result.Cell.ID, DatasetID: result.Cell.DatasetID,
			Identities: result.Cell.Cardinality.Identities, Models: result.Cell.Cardinality.Models, APIKeys: result.Cell.Cardinality.APIKeys,
			ResourceID: result.Cell.Resource.ID, CPU: result.Cell.Resource.CPU, MemoryMiB: result.Cell.Resource.MemoryMiB,
			Status: result.Status, StartupSeconds: result.StartupSeconds, WarmupSeconds: result.WarmupSeconds,
			HardEventsPerSecond: result.Capacity.HardEventsPerSecond, InteractiveEventsPerSecond: result.Capacity.InteractiveEventsPerSecond,
			RecommendedEventsPerSecond: result.Capacity.RecommendedEventsPerSecond,
			PeakMemoryBytes:            max(result.PeakResource.MemoryPeakBytes, result.LastResource.MemoryPeakBytes), CapacityPeakMemoryBytes: capacityPeakMemory,
			CapacityCPUUtilizationPercent: cpuUtilization, BoundaryHTTPP95MS: p95, BoundaryHTTPP99MS: p99,
			DashboardStatus:      dashboardStatus(result.Capacity.HardEventsPerSecond, result.Capacity.InteractiveEventsPerSecond),
			SlowestDashboardPath: slowestPath, SlowestDashboardP99MS: slowestP99,
			SharedDriver: result.SharedDriver, Error: result.Error,
		}
		if result.Status != "completed" {
			summary.Failures++
		}
		summary.Cells = append(summary.Cells, cell)
	}
	sort.Slice(summary.Cells, func(left, right int) bool {
		if summary.Cells[left].CPU != summary.Cells[right].CPU {
			return summary.Cells[left].CPU < summary.Cells[right].CPU
		}
		if summary.Cells[left].MemoryMiB != summary.Cells[right].MemoryMiB {
			return summary.Cells[left].MemoryMiB < summary.Cells[right].MemoryMiB
		}
		return summary.Cells[left].CellID < summary.Cells[right].CellID
	})
	summary.Hardware = buildHardwareRecommendations(summary.Cells)
	return summary
}

func buildHardwareRecommendations(cells []SummaryCell) []HardwareRecommendation {
	recommendations := make([]HardwareRecommendation, 0, len(cells))
	for _, cell := range cells {
		recommendation := HardwareRecommendation{
			DatasetID: cell.DatasetID, ResourceID: cell.ResourceID, CPU: cell.CPU, MemoryMiB: cell.MemoryMiB,
			IngestionMaxEventsPerSecond: cell.HardEventsPerSecond, DashboardMaxEventsPerSecond: cell.InteractiveEventsPerSecond,
			RecommendedEventsPerSecond: cell.RecommendedEventsPerSecond, CPUUtilizationPercent: cell.CapacityCPUUtilizationPercent,
			CoreP95MS: cell.BoundaryHTTPP95MS, OverallP99MS: cell.BoundaryHTTPP99MS,
			PeakMemoryBytes: cell.CapacityPeakMemoryBytes, SharedDriver: cell.SharedDriver,
		}
		if recommendation.PeakMemoryBytes == 0 {
			recommendation.PeakMemoryBytes = cell.PeakMemoryBytes
		}
		switch {
		case cell.Status != "completed":
			recommendation.Guidance = "该资源档未完成，不能用于容量建议"
		case cell.HardEventsPerSecond <= 0:
			recommendation.Guidance = "Keeper 可以启动，但未找到可持续 ingestion 容量"
		case cell.InteractiveEventsPerSecond <= 0:
			recommendation.Guidance = fmt.Sprintf("可持续 ingestion 上限为 %d events/s；Dashboard 未通过交互 SLA", cell.HardEventsPerSecond)
		default:
			recommendation.Guidance = fmt.Sprintf("持续流量建议不超过 %d events/s", cell.RecommendedEventsPerSecond)
		}
		recommendations = append(recommendations, recommendation)
	}
	return recommendations
}

func capacityLatency(attempts []ProbeAttempt, hardCapacity int) (float64, float64, string, float64) {
	selected := capacityAttempts(attempts, hardCapacity)
	var p95Values, p99Values []float64
	slowestPath := ""
	slowestP99 := 0.0
	for _, attempt := range selected {
		p95Values = append(p95Values, attempt.Report.Metrics.HTTPP95MS)
		p99Values = append(p99Values, attempt.Report.Metrics.HTTPP99MS)
		for path, latency := range attempt.Report.LatencyByPath {
			if latency.P99MS > slowestP99 {
				slowestPath = path
				slowestP99 = latency.P99MS
			}
		}
	}
	return medianFloat(p95Values), medianFloat(p99Values), slowestPath, slowestP99
}

func capacityTelemetry(attempts []ProbeAttempt, hardCapacity int) (float64, int64) {
	selected := capacityAttempts(attempts, hardCapacity)
	var utilization []float64
	var peakMemory int64
	for _, attempt := range selected {
		utilization = append(utilization, attempt.Resource.CPUUtilizationPercent)
		peakMemory = max(peakMemory, attempt.PeakResource.MemoryPeakBytes, attempt.Resource.MemoryPeakBytes)
	}
	return medianFloat(utilization), peakMemory
}

func capacityAttempts(attempts []ProbeAttempt, hardCapacity int) []ProbeAttempt {
	selected := make([]ProbeAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.Phase == "boundary" && attempt.RatePerSecond == hardCapacity && attempt.Report.Evaluation.HardPass {
			selected = append(selected, attempt)
		}
	}
	if len(selected) == 0 {
		for _, attempt := range attempts {
			if attempt.RatePerSecond == hardCapacity && attempt.Report.Evaluation.HardPass && (attempt.Phase == "search" || attempt.Phase == "soak") {
				selected = append(selected, attempt)
			}
		}
	}
	return selected
}

func dashboardStatus(hardCapacity, interactiveCapacity int) string {
	switch {
	case hardCapacity <= 0:
		return "no_capacity"
	case interactiveCapacity >= hardCapacity:
		return "interactive"
	case interactiveCapacity > 0:
		return "interactive_below_ingestion"
	default:
		return "ingestion_only"
	}
}

func medianFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[middle]
	}
	return (ordered[middle-1] + ordered[middle]) / 2
}

func loadCellResults(runDir string) ([]CellResult, error) {
	paths, err := filepath.Glob(filepath.Join(runDir, "cells", "*", "result.json"))
	if err != nil {
		return nil, err
	}
	var results []CellResult
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var result CellResult
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("decode cell result %s: %w", path, err)
		}
		results = append(results, result)
	}
	sort.Slice(results, func(left, right int) bool { return results[left].Cell.ID < results[right].Cell.ID })
	return results, nil
}

func RebuildResultsJSONL(runDir string) error {
	results, err := loadCellResults(runDir)
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, result := range results {
		if err := encoder.Encode(result); err != nil {
			return err
		}
	}
	return writeBytesAtomic(filepath.Join(runDir, "results.jsonl"), buffer.Bytes())
}

func writeSummaryCSV(path string, summary RunSummary) error {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{
		"cell_id", "dataset_id", "identities", "models", "api_keys", "resource_id", "cpu", "memory_mib",
		"status", "startup_seconds", "warmup_seconds", "hard_events_per_second", "interactive_events_per_second", "recommended_events_per_second",
		"peak_memory_bytes", "capacity_peak_memory_bytes", "capacity_cpu_utilization_percent", "capacity_http_core_p95_ms", "capacity_http_overall_p99_ms",
		"dashboard_status", "slowest_dashboard_path", "slowest_dashboard_p99_ms", "shared_driver", "error",
	})
	for _, cell := range summary.Cells {
		_ = writer.Write([]string{
			cell.CellID, cell.DatasetID, strconv.Itoa(cell.Identities), strconv.Itoa(cell.Models), strconv.Itoa(cell.APIKeys),
			cell.ResourceID, strconv.FormatFloat(cell.CPU, 'f', -1, 64), strconv.Itoa(cell.MemoryMiB), cell.Status,
			strconv.FormatFloat(cell.StartupSeconds, 'f', 3, 64), strconv.FormatFloat(cell.WarmupSeconds, 'f', 3, 64),
			strconv.Itoa(cell.HardEventsPerSecond), strconv.Itoa(cell.InteractiveEventsPerSecond), strconv.Itoa(cell.RecommendedEventsPerSecond),
			strconv.FormatInt(cell.PeakMemoryBytes, 10), strconv.FormatInt(cell.CapacityPeakMemoryBytes, 10), strconv.FormatFloat(cell.CapacityCPUUtilizationPercent, 'f', 3, 64),
			strconv.FormatFloat(cell.BoundaryHTTPP95MS, 'f', 3, 64), strconv.FormatFloat(cell.BoundaryHTTPP99MS, 'f', 3, 64),
			cell.DashboardStatus, cell.SlowestDashboardPath, strconv.FormatFloat(cell.SlowestDashboardP99MS, 'f', 3, 64), strconv.FormatBool(cell.SharedDriver), cell.Error,
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return writeBytesAtomic(path, buffer.Bytes())
}

func writeReportMarkdown(path string, summary RunSummary) error {
	var report strings.Builder
	report.WriteString("# CPA Usage Keeper Capacity Benchmark\n\n")
	report.WriteString("> 容量结果与建议适用于本次记录的 linux/amd64 硬件规格、数据集指纹和 Keeper 二进制。\n\n")
	report.WriteString("## Hardware recommendations\n\n")
	report.WriteString("| Dataset | Resource | Ingestion max | Dashboard max | Recommended | CPU avg | Core p95 / overall p99 | Peak memory | Note |\n")
	report.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
	for _, item := range summary.Hardware {
		note := item.Guidance
		if item.SharedDriver {
			note += "；Keeper 与负载器共享宿主 CPU，结果偏保守"
		}
		report.WriteString(fmt.Sprintf("| %s | %s | %d | %d | %d | %.1f%% | %.1f / %.1f ms | %.1f MiB | %s |\n",
			item.DatasetID, item.ResourceID, item.IngestionMaxEventsPerSecond, item.DashboardMaxEventsPerSecond, item.RecommendedEventsPerSecond,
			item.CPUUtilizationPercent, item.CoreP95MS, item.OverallP99MS, float64(item.PeakMemoryBytes)/(1024*1024), note))
	}
	report.WriteString("\n## Cells\n\n")
	report.WriteString("| Cell | Cardinality I/M/K | Resource | Status | Ingestion max | Dashboard max | CPU avg / peak memory | Dashboard status | Core p95 / overall p99 | Slowest endpoint p99 |\n")
	report.WriteString("| --- | ---: | --- | --- | ---: | ---: | ---: | --- | ---: | --- |\n")
	for _, cell := range summary.Cells {
		slowest := "-"
		if cell.SlowestDashboardPath != "" {
			slowest = fmt.Sprintf("%s %.1f ms", cell.SlowestDashboardPath, cell.SlowestDashboardP99MS)
		}
		report.WriteString(fmt.Sprintf("| %s | %d/%d/%d | %s | %s | %d | %d | %.1f%% / %.1f MiB | %s | %.1f / %.1f ms | %s |\n",
			cell.CellID, cell.Identities, cell.Models, cell.APIKeys, cell.ResourceID, cell.Status,
			cell.HardEventsPerSecond, cell.InteractiveEventsPerSecond, cell.CapacityCPUUtilizationPercent, float64(cell.CapacityPeakMemoryBytes)/(1024*1024),
			cell.DashboardStatus, cell.BoundaryHTTPP95MS, cell.BoundaryHTTPP99MS, slowest))
	}
	if summary.Failures > 0 {
		report.WriteString(fmt.Sprintf("\nFailures: %d. Inspect each cell's result and logs before using recommendations.\n", summary.Failures))
	}
	return writeBytesAtomic(path, []byte(report.String()))
}

func writeBytesAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".benchmark-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
