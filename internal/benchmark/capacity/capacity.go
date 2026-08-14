package capacity

import (
	"fmt"
)

type CapacitySearch struct {
	rates         []int
	lowPassIndex  int
	highFailIndex int
	pendingIndex  int
	pending       int
	startIndex    int
	started       bool
	ramping       bool
	done          bool
}

func NewCapacitySearch(rates []int) (*CapacitySearch, error) {
	if len(rates) == 0 {
		return nil, fmt.Errorf("capacity rates are required")
	}
	return NewCapacitySearchAt(rates, rates[0])
}

func NewCapacitySearchAt(rates []int, initialRate int) (*CapacitySearch, error) {
	copyRates := append([]int(nil), rates...)
	startIndex := -1
	for index, rate := range copyRates {
		if rate <= 0 || (index > 0 && rate <= copyRates[index-1]) {
			return nil, fmt.Errorf("capacity rates must be positive and increasing")
		}
		if rate == initialRate {
			startIndex = index
		}
	}
	if startIndex < 0 {
		return nil, fmt.Errorf("initial capacity rate %d is not configured", initialRate)
	}
	return &CapacitySearch{
		rates: copyRates, lowPassIndex: -1, highFailIndex: len(copyRates), pendingIndex: -1, startIndex: startIndex, ramping: true,
	}, nil
}

func (search *CapacitySearch) Next() (int, bool) {
	if search == nil || search.done || search.pending != 0 {
		return 0, false
	}
	if search.highFailIndex-search.lowPassIndex <= 1 {
		search.done = true
		return 0, false
	}
	if !search.started {
		search.pendingIndex = search.startIndex
		search.started = true
	} else if search.ramping && search.highFailIndex == len(search.rates) {
		if search.lowPassIndex < 0 {
			search.pendingIndex = 0
		} else {
			search.pendingIndex = len(search.rates) - 1
			current := search.rates[search.lowPassIndex]
			for index := search.lowPassIndex + 1; index < len(search.rates); index++ {
				if current <= search.rates[len(search.rates)-1]/2 && search.rates[index] >= current*2 {
					search.pendingIndex = index
					break
				}
			}
		}
	} else {
		search.pendingIndex = search.lowPassIndex + (search.highFailIndex-search.lowPassIndex)/2
	}
	search.pending = search.rates[search.pendingIndex]
	return search.pending, true
}

func (search *CapacitySearch) Record(rate int, passed bool) {
	if search == nil || search.done || search.pending == 0 || rate != search.pending {
		return
	}
	search.pending = 0
	if passed {
		search.lowPassIndex = search.pendingIndex
	} else {
		search.highFailIndex = search.pendingIndex
		search.ramping = false
	}
	search.pendingIndex = -1
	if search.highFailIndex-search.lowPassIndex <= 1 {
		search.done = true
	}
}

func (search *CapacitySearch) HardCapacity() int {
	if search == nil {
		return 0
	}
	if search.lowPassIndex < 0 {
		return 0
	}
	return search.rates[search.lowPassIndex]
}

func (search *CapacitySearch) FailureBoundary() int {
	if search == nil || search.highFailIndex < 0 || search.highFailIndex >= len(search.rates) {
		return 0
	}
	return search.rates[search.highFailIndex]
}

// PromoteFailure resumes the upward ramp when a longer boundary probe proves
// that the prior short-probe failure was transient.
func (search *CapacitySearch) PromoteFailure(rate int) error {
	if search == nil || search.pending != 0 || search.FailureBoundary() != rate {
		return fmt.Errorf("capacity failure boundary %d cannot be promoted", rate)
	}
	search.lowPassIndex = search.highFailIndex
	search.highFailIndex = len(search.rates)
	search.pendingIndex = -1
	search.ramping = true
	search.done = search.lowPassIndex == len(search.rates)-1
	return nil
}

type ProbeMetrics struct {
	OfferedEvents           int64   `json:"offered_events"`
	PublishedEvents         int64   `json:"published_events"`
	DurableEvents           int64   `json:"durable_events"`
	BacklogStart            int64   `json:"backlog_start"`
	BacklogEnd              int64   `json:"backlog_end"`
	Errors                  int64   `json:"errors"`
	HTTPRequests            int64   `json:"http_requests"`
	CoreHTTPRequests        int64   `json:"core_http_requests"`
	CoreHTTPErrors          int64   `json:"core_http_errors"`
	AnalysisLatencyRequests int64   `json:"analysis_latency_requests"`
	AnalysisLatencyErrors   int64   `json:"analysis_latency_errors"`
	HTTPP95MS               float64 `json:"http_p95_ms"`
	HTTPP99MS               float64 `json:"http_p99_ms"`
	OOM                     bool    `json:"oom"`
	Panic                   bool    `json:"panic"`
	CheckpointLag           int64   `json:"checkpoint_lag"`
	IdentityPending         int64   `json:"identity_pending"`
	DrainSeconds            float64 `json:"drain_seconds"`
}

type ProbeThresholds struct {
	MinPublishedRatio float64 `json:"min_published_ratio"`
	MinDurableRatio   float64 `json:"min_durable_ratio"`
	MaxBacklogGrowth  int64   `json:"max_backlog_growth"`
	InteractiveP95MS  float64 `json:"interactive_p95_ms"`
	InteractiveP99MS  float64 `json:"interactive_p99_ms"`
	MaxDrainSeconds   float64 `json:"max_drain_seconds"`
}

type ProbeEvaluation struct {
	HardPass            bool     `json:"hard_pass"`
	InteractivePass     bool     `json:"interactive_pass"`
	AnalysisLatencyPass bool     `json:"analysis_latency_pass"`
	Reasons             []string `json:"reasons,omitempty"`
}

func EvaluateProbe(metrics ProbeMetrics, thresholds ProbeThresholds) ProbeEvaluation {
	if thresholds.MinPublishedRatio <= 0 {
		thresholds.MinPublishedRatio = 0.999
	}
	if thresholds.MinDurableRatio <= 0 {
		thresholds.MinDurableRatio = 0.99
	}
	evaluation := ProbeEvaluation{HardPass: true, AnalysisLatencyPass: true}
	if metrics.OOM {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "oom")
	}
	if metrics.Panic {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "panic")
	}
	if metrics.Errors > 0 {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "errors")
	}
	if thresholds.MaxDrainSeconds > 0 && metrics.DrainSeconds > thresholds.MaxDrainSeconds {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "drain_lag")
	}
	if metrics.OfferedEvents > 0 && float64(metrics.PublishedEvents)/float64(metrics.OfferedEvents) < thresholds.MinPublishedRatio {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "driver_lag")
	}
	durableTailAllowed := metrics.OfferedEvents >= 10 &&
		metrics.PublishedEvents == metrics.OfferedEvents &&
		metrics.OfferedEvents-metrics.DurableEvents == 1 &&
		metrics.BacklogEnd <= metrics.BacklogStart &&
		metrics.CheckpointLag == 0 &&
		metrics.IdentityPending == 0
	if metrics.OfferedEvents > 0 &&
		float64(metrics.DurableEvents)/float64(metrics.OfferedEvents) < thresholds.MinDurableRatio &&
		!durableTailAllowed {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "durable_throughput")
	}
	if metrics.BacklogEnd-metrics.BacklogStart > thresholds.MaxBacklogGrowth {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "backlog_growth")
	}
	if metrics.CheckpointLag > 0 {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "checkpoint_lag")
	}
	if metrics.IdentityPending > 0 {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "identity_pending")
	}
	evaluation.InteractivePass = evaluation.HardPass
	// 诊断状态只反映自身请求与会使整个 Keeper 失效的运行时故障，不继承 ingestion 容量失败。
	evaluation.AnalysisLatencyPass = !metrics.OOM && !metrics.Panic
	if metrics.CoreHTTPErrors > 0 {
		evaluation.InteractivePass = false
		evaluation.Reasons = append(evaluation.Reasons, "core_http_errors")
	}
	if metrics.AnalysisLatencyErrors > 0 {
		evaluation.AnalysisLatencyPass = false
		evaluation.Reasons = append(evaluation.Reasons, "analysis_latency_errors")
	}
	if metrics.HTTPRequests > 0 {
		if thresholds.InteractiveP95MS > 0 && metrics.HTTPP95MS > thresholds.InteractiveP95MS {
			evaluation.InteractivePass = false
			evaluation.Reasons = append(evaluation.Reasons, "http_p95")
		}
		if thresholds.InteractiveP99MS > 0 && metrics.HTTPP99MS > thresholds.InteractiveP99MS {
			evaluation.InteractivePass = false
			evaluation.Reasons = append(evaluation.Reasons, "http_p99")
		}
	}
	return evaluation
}
