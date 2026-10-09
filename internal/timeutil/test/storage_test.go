package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/timeutil"
)

func TestFormatStorageTimeUsesProjectTimezoneOffset(t *testing.T) {
	useTimezone(t, "Asia/Shanghai")

	input := time.Date(2026, 5, 12, 13, 59, 18, 353569620, time.UTC)

	got := timeutil.FormatStorageTime(input)

	if got != "2026-05-12T21:59:18.35356962+08:00" {
		t.Fatalf("expected Asia/Shanghai RFC3339Nano storage time, got %q", got)
	}
}

func TestParseStorageTime(t *testing.T) {
	useTimezone(t, "Asia/Shanghai")
	for _, test := range []struct{ input, want string }{
		{"2026-05-12 13:47:39.744240399+00:00", "2026-05-12T21:47:39.744240399+08:00"},
		{"2026-05-12T13:47:39.744240399Z", "2026-05-12T21:47:39.744240399+08:00"},
		{"2026-05-12 13:47:39.744240399", "2026-05-12T13:47:39.744240399+08:00"},
		{"2026-05-12T13:47:39.744240399", "2026-05-12T13:47:39.744240399+08:00"},
	} {
		t.Run(test.input, func(t *testing.T) {
			parsed, err := timeutil.ParseStorageTime(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got := timeutil.FormatStorageTime(parsed); got != test.want {
				t.Fatalf("formatted time = %q, want %q", got, test.want)
			}
		})
	}
}
