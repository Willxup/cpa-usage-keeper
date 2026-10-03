package cpa

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type QuotaObservation struct {
	AuthIndex  string      `json:"auth_index"`
	Provider   string      `json:"provider"`
	Window     string      `json:"window"`
	Headers    http.Header `json:"headers"`
	ObservedAt time.Time   `json:"observed_at"`
	Source     string      `json:"source"`
}

func (c *Client) FetchQuotaObservations(ctx context.Context) ([]QuotaObservation, error) {
	var result struct {
		Items []QuotaObservation `json:"items"`
	}
	status, _, err := c.doManagementJSONRequest(ctx, "/v8/management/quota/observations", &result, "quota observations")
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("quota cache status %d", status)
	}
	return result.Items, nil
}
