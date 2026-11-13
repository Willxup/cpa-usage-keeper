package test

import (
	"encoding/json"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/credentials"
)

func TestCredentialDecodesNullableSIWCFields(t *testing.T) {
	tests := []struct {
		name, input      string
		bind, bound, use *bool
		status           *string
		expires          *time.Time
	}{
		{"true and false", `{"siwc_bind_supported":true,"siwc_bound":false,"use_siwc":true,"siwc_status":"valid","siwc_expires_at":"2026-11-01T12:34:56+08:00"}`, new(true), new(false), new(true), new("valid"), new(time.Date(2026, 11, 1, 12, 34, 56, 0, time.FixedZone("", 8*3600)))},
		{"opposite booleans", `{"siwc_bind_supported":false,"siwc_bound":true,"use_siwc":false,"siwc_status":"future"}`, new(false), new(true), new(false), new("future"), nil},
		{"explicit null", `{"siwc_bind_supported":null,"siwc_bound":null,"use_siwc":null,"siwc_status":null,"siwc_expires_at":null}`, nil, nil, nil, nil, nil},
		{"missing", `{}`, nil, nil, nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var file credentials.Credential
			if err := json.Unmarshal([]byte(tt.input), &file); err != nil {
				t.Fatal(err)
			}
			if !equalSIWCValue(file.SIWCBindSupported, tt.bind) || !equalSIWCValue(file.SIWCBound, tt.bound) || !equalSIWCValue(file.UseSIWC, tt.use) || !equalSIWCValue(file.SIWCStatus, tt.status) {
				t.Fatalf("decoded SIWC fields: %+v", file)
			}
			if (file.SIWCExpiresAt == nil) != (tt.expires == nil) || (file.SIWCExpiresAt != nil && !file.SIWCExpiresAt.Equal(*tt.expires)) {
				t.Fatalf("decoded expiry = %v, want %v", file.SIWCExpiresAt, tt.expires)
			}
		})
	}
	var file credentials.Credential
	if err := json.Unmarshal([]byte(`{"siwc_expires_at":"not-a-time"}`), &file); err == nil {
		t.Fatal("malformed expiry must fail decoding like other credential timestamps")
	}
}

func equalSIWCValue[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
