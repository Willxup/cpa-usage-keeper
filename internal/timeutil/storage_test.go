package timeutil

import (
	"testing"
	"time"
)

func TestFormatStorageTimeUsesProjectTimezoneOffset(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	input := time.Date(2026, 5, 12, 13, 59, 18, 353569620, time.UTC)

	got := FormatStorageTime(input)

	if got != "2026-05-12T21:59:18.35356962+08:00" {
		t.Fatalf("expected Asia/Shanghai RFC3339Nano storage time, got %q", got)
	}
}

func TestParseStorageTimeUsesEmbeddedOffsetWhenPresent(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	for _, input := range []string{
		"2026-05-12 13:47:39.744240399+00:00",
		"2026-05-12T13:47:39.744240399Z",
	} {
		parsed, err := ParseStorageTime(input)
		if err != nil {
			t.Fatalf("ParseStorageTime(%q) returned error: %v", input, err)
		}
		got := FormatStorageTime(parsed)
		if got != "2026-05-12T21:47:39.744240399+08:00" {
			t.Fatalf("expected %q to convert from embedded offset, got %q", input, got)
		}
	}
}

func TestParseStorageTimeUsesProjectTimezoneForNaiveValues(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	for _, input := range []string{
		"2026-05-12 13:47:39.744240399",
		"2026-05-12T13:47:39.744240399",
	} {
		parsed, err := ParseStorageTime(input)
		if err != nil {
			t.Fatalf("ParseStorageTime(%q) returned error: %v", input, err)
		}
		got := FormatStorageTime(parsed)
		if got != "2026-05-12T13:47:39.744240399+08:00" {
			t.Fatalf("expected %q to be interpreted in project timezone, got %q", input, got)
		}
	}
}
