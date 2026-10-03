package cpa

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (c *Client) UpdateAPIKeyName(ctx context.Context, key, name string) error {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 128 || (key != "" && strings.Contains(name, key)) {
		return fmt.Errorf("invalid shared key name")
	}
	for _, char := range name {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return fmt.Errorf("invalid shared key name")
		}
	}
	status, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, "/v8/management/access/api-key-names", map[string]any{
		"names": map[string]string{fmt.Sprintf("%x", sha256.Sum256([]byte(key))): name},
	}, nil, "key names")
	// Older CPA versions retain Keeper's existing local alias editing behavior.
	// Other failures must not be treated as a successful shared update.
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *Client) FetchAPIKeyNames(ctx context.Context) (map[string]string, error) {
	var result struct {
		Names map[string]string `json:"names"`
	}
	status, _, err := c.doManagementJSONRequest(ctx, "/v8/management/access/api-key-names", &result, "key names")
	if status == http.StatusNotFound {
		return nil, nil
	}
	return result.Names, err
}
