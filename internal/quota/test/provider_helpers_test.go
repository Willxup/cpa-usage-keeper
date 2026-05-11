package test

import (
	"context"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type recordingManagementCaller struct {
	requests  []apicall.Request
	responses []*apicall.Response
}

func (c *recordingManagementCaller) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	c.requests = append(c.requests, request)
	if len(c.responses) == 0 {
		return &apicall.Response{StatusCode: 200}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func stringPtr(value string) *string {
	return &value
}

func contains(value, substr string) bool {
	return strings.Contains(value, substr)
}
