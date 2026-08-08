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
	done          bool
}

func NewCapacitySearch(rates []int) (*CapacitySearch, error) {
	if len(rates) == 0 {
		return nil, fmt.Errorf("capacity rates are required")
	}
	copyRates := append([]int(nil), rates...)
	for index, rate := range copyRates {
		if rate <= 0 || (index > 0 && rate <= copyRates[index-1]) {
			return nil, fmt.Errorf("capacity rates must be positive and increasing")
		}
	}
	return &CapacitySearch{
		rates: copyRates, lowPassIndex: -1, highFailIndex: len(copyRates), pendingIndex: -1,
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
	search.pendingIndex = search.lowPassIndex + (search.highFailIndex-search.lowPassIndex)/2
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

type ProbeMetrics struct {
	OfferedEvents   int64   `json:"offered_events"`
	PublishedEvents int64   `json:"published_events"`
	DurableEvents   int64   `json:"durable_events"`
	BacklogStart    int64   `json:"backlog_start"`
	BacklogEnd      int64   `json:"backlog_end"`
	Errors          int64   `json:"errors"`
	HTTPRequests    int64   `json:"http_requests"`
	HTTPP95MS       float64 `json:"http_p95_ms"`
	HTTPP99MS       float64 `json:"http_p99_ms"`
	OOM             bool    `json:"oom"`
	Panic           bool    `json:"panic"`
	SQLiteBusy      int64   `json:"sqlite_busy"`
	CheckpointLag   int64   `json:"checkpoint_lag"`
	IdentityPending int64   `json:"identity_pending"`
}

type ProbeThresholds struct {
	MinPublishedRatio float64 `json:"min_published_ratio"`
	MinDurableRatio   float64 `json:"min_durable_ratio"`
	MaxBacklogGrowth  int64   `json:"max_backlog_growth"`
	InteractiveP95MS  float64 `json:"interactive_p95_ms"`
	InteractiveP99MS  float64 `json:"interactive_p99_ms"`
}

type ProbeEvaluation struct {
	HardPass        bool     `json:"hard_pass"`
	InteractivePass bool     `json:"interactive_pass"`
	Reasons         []string `json:"reasons,omitempty"`
}

func EvaluateProbe(metrics ProbeMetrics, thresholds ProbeThresholds) ProbeEvaluation {
	if thresholds.MinPublishedRatio <= 0 {
		thresholds.MinPublishedRatio = 0.999
	}
	if thresholds.MinDurableRatio <= 0 {
		thresholds.MinDurableRatio = 0.99
	}
	evaluation := ProbeEvaluation{HardPass: true}
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
	if metrics.OfferedEvents > 0 && float64(metrics.PublishedEvents)/float64(metrics.OfferedEvents) < thresholds.MinPublishedRatio {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "driver_lag")
	}
	if metrics.SQLiteBusy > 0 {
		evaluation.HardPass = false
		evaluation.Reasons = append(evaluation.Reasons, "sqlite_busy")
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
