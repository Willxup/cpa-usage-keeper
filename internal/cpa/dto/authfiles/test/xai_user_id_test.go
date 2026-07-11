package test

import (
	"encoding/json"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

func TestAuthFilePreservesXAIUserIDCandidates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		path  []string
		want  string
	}{
		{name: "top level sub", input: `{"sub":"top-sub"}`, path: []string{"sub"}, want: "top-sub"},
		{name: "top level subject", input: `{"subject":"top-subject"}`, path: []string{"subject"}, want: "top-subject"},
		{name: "top level user_id", input: `{"user_id":"top-user-id"}`, path: []string{"user_id"}, want: "top-user-id"},
		{name: "top level userId", input: `{"userId":"top-user-id-camel"}`, path: []string{"userId"}, want: "top-user-id-camel"},
		{name: "metadata sub", input: `{"metadata":{"sub":"metadata-sub"}}`, path: []string{"metadata", "sub"}, want: "metadata-sub"},
		{name: "metadata subject", input: `{"metadata":{"subject":"metadata-subject"}}`, path: []string{"metadata", "subject"}, want: "metadata-subject"},
		{name: "metadata user_id", input: `{"metadata":{"user_id":"metadata-user-id"}}`, path: []string{"metadata", "user_id"}, want: "metadata-user-id"},
		{name: "metadata userId", input: `{"metadata":{"userId":"metadata-user-id-camel"}}`, path: []string{"metadata", "userId"}, want: "metadata-user-id-camel"},
		{name: "attributes sub", input: `{"attributes":{"sub":"attributes-sub"}}`, path: []string{"attributes", "sub"}, want: "attributes-sub"},
		{name: "attributes subject", input: `{"attributes":{"subject":"attributes-subject"}}`, path: []string{"attributes", "subject"}, want: "attributes-subject"},
		{name: "attributes user_id", input: `{"attributes":{"user_id":"attributes-user-id"}}`, path: []string{"attributes", "user_id"}, want: "attributes-user-id"},
		{name: "attributes userId", input: `{"attributes":{"userId":"attributes-user-id"}}`, path: []string{"attributes", "userId"}, want: "attributes-user-id"},
		{name: "oauth sub", input: `{"oauth":{"sub":"oauth-sub"}}`, path: []string{"oauth", "sub"}, want: "oauth-sub"},
		{name: "oauth subject", input: `{"oauth":{"subject":"oauth-subject"}}`, path: []string{"oauth", "subject"}, want: "oauth-subject"},
		{name: "metadata oauth", input: `{"metadata":{"oauth":{"sub":"metadata-oauth-sub"}}}`, path: []string{"metadata", "oauth", "sub"}, want: "metadata-oauth-sub"},
		{name: "attributes oauth", input: `{"attributes":{"oauth":{"subject":"attributes-oauth-subject"}}}`, path: []string{"attributes", "oauth", "subject"}, want: "attributes-oauth-subject"},
		{name: "user sub", input: `{"user":{"sub":"user-sub"}}`, path: []string{"user", "sub"}, want: "user-sub"},
		{name: "user id", input: `{"user":{"id":"user-id"}}`, path: []string{"user", "id"}, want: "user-id"},
		{name: "metadata user", input: `{"metadata":{"user":{"sub":"metadata-user-sub"}}}`, path: []string{"metadata", "user", "sub"}, want: "metadata-user-sub"},
		{name: "attributes user", input: `{"attributes":{"user":{"id":"attributes-user-id"}}}`, path: []string{"attributes", "user", "id"}, want: "attributes-user-id"},
		{name: "numeric candidate", input: `{"sub":12345}`, path: []string{"sub"}, want: "12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var file authfiles.AuthFile
			if err := json.Unmarshal([]byte(tt.input), &file); err != nil {
				t.Fatalf("unmarshal AuthFile: %v", err)
			}
			encoded, err := json.Marshal(file)
			if err != nil {
				t.Fatalf("marshal AuthFile: %v", err)
			}
			var roundTrip map[string]any
			if err := json.Unmarshal(encoded, &roundTrip); err != nil {
				t.Fatalf("unmarshal round-trip AuthFile: %v", err)
			}
			if got := nestedString(roundTrip, tt.path...); got != tt.want {
				t.Fatalf("expected %v to survive AuthFile decoding, got %q from %s", tt.path, got, encoded)
			}
		})
	}
}

func nestedString(value map[string]any, path ...string) string {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	result, _ := current.(string)
	return result
}
