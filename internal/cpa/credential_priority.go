package cpa

import (
	"context"
	"net/http"
)

type credentialPriorityPatchRequest struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

func (c *Client) UpdateCredentialPriority(ctx context.Context, name string, priority int) (int, error) {
	statusCode, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, cpaManagementCredentialsFieldsEndpoint,
		credentialPriorityPatchRequest{Name: name, Priority: priority}, nil, "credential priority")
	return statusCode, err
}
