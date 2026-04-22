package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/service"
)

var presetUsageRangeDurations = map[string]time.Duration{
	"4h":  4 * time.Hour,
	"8h":  8 * time.Hour,
	"12h": 12 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

func parseUsageFilterQuery(req *http.Request, anchor time.Time) (service.UsageFilter, error) {
	if req == nil {
		return service.UsageFilter{}, nil
	}

	rangeValue := strings.TrimSpace(req.URL.Query().Get("range"))
	if rangeValue == "" {
		rangeValue = "all"
	}

	filter := service.UsageFilter{Range: rangeValue}
	switch rangeValue {
	case "all":
		return filter, nil
	case "custom":
		startValue := strings.TrimSpace(req.URL.Query().Get("start"))
		endValue := strings.TrimSpace(req.URL.Query().Get("end"))
		if startValue == "" || endValue == "" {
			return service.UsageFilter{}, fmt.Errorf("custom range requires start and end")
		}
		startTime, err := time.Parse(time.RFC3339, startValue)
		if err != nil {
			return service.UsageFilter{}, fmt.Errorf("invalid start: %w", err)
		}
		endTime, err := time.Parse(time.RFC3339, endValue)
		if err != nil {
			return service.UsageFilter{}, fmt.Errorf("invalid end: %w", err)
		}
		startTime = startTime.UTC()
		endTime = endTime.UTC()
		if startTime.After(endTime) {
			return service.UsageFilter{}, fmt.Errorf("custom range start must be before end")
		}
		filter.StartTime = &startTime
		filter.EndTime = &endTime
		return filter, nil
	default:
		duration, ok := presetUsageRangeDurations[rangeValue]
		if !ok {
			return service.UsageFilter{}, fmt.Errorf("unsupported usage range %q", rangeValue)
		}
		endTime := anchor.UTC()
		startTime := endTime.Add(-duration)
		filter.StartTime = &startTime
		filter.EndTime = &endTime
		return filter, nil
	}
}
