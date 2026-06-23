package quota

import (
	"math"
	"net/http"
	"strconv"
	"testing"
)

func TestParseCodexHeaderQuotaParsesPrimarySecondaryAndAdditionalWindows(t *testing.T) {
	headers := http.Header{
		"X-Codex-Plan-Type":                                      []string{"pro"},
		"X-Codex-Primary-Used-Percent":                           []string{"4"},
		"X-Codex-Primary-Window-Minutes":                         []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds":                    []string{"7404"},
		"X-Codex-Primary-Reset-At":                               []string{"1782105247"},
		"X-Codex-Secondary-Used-Percent":                         []string{"22"},
		"X-Codex-Secondary-Window-Minutes":                       []string{"10080"},
		"X-Codex-Secondary-Reset-After-Seconds":                  []string{"303127"},
		"X-Codex-Secondary-Reset-At":                             []string{"1782400970"},
		"X-Codex-Bengalfox-Limit-Name":                           []string{"GPT-5.3-Codex-Spark"},
		"X-Codex-Bengalfox-Primary-Used-Percent":                 []string{"0"},
		"X-Codex-Bengalfox-Primary-Window-Minutes":               []string{"300"},
		"X-Codex-Bengalfox-Primary-Reset-After-Seconds":          []string{"18000"},
		"X-Codex-Bengalfox-Primary-Reset-At":                     []string{"1782115844"},
		"X-Codex-Bengalfox-Secondary-Used-Percent":               []string{"0"},
		"X-Codex-Bengalfox-Secondary-Window-Minutes":             []string{"10080"},
		"X-Codex-Bengalfox-Secondary-Reset-After-Seconds":        []string{"604800"},
		"X-Codex-Bengalfox-Secondary-Reset-At":                   []string{"1782702643"},
		"X-Codex-Bengalfox-Primary-Over-Secondary-Limit-Percent": []string{"0"},
		"X-Codex-Credits-Has-Credits":                            []string{"False"},
	}

	output, ok := parseCodexHeaderQuota(headers)
	if !ok {
		t.Fatal("expected codex header quota to parse")
	}
	rows := NormalizeQuotaRows(output)
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %#v", rows)
	}
	if rows[0].Key != "rate_limit.primary_window" || rows[0].PlanType != "pro" || rows[0].Window == nil || rows[0].Window.Seconds == nil || *rows[0].Window.Seconds != quotaWindowFiveHourSeconds {
		t.Fatalf("unexpected primary row: %#v", rows[0])
	}
	if rows[0].UsedPercent == nil || *rows[0].UsedPercent != 4 || rows[0].ResetAfterSeconds == nil || *rows[0].ResetAfterSeconds != 7404 {
		t.Fatalf("unexpected primary usage fields: %#v", rows[0])
	}
	if rows[1].Key != "rate_limit.secondary_window" || rows[1].Window == nil || rows[1].Window.Seconds == nil || *rows[1].Window.Seconds != quotaWindowSevenDaySeconds {
		t.Fatalf("unexpected secondary row: %#v", rows[1])
	}
	if rows[2].Key != "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window" || rows[2].Scope != "additional" || rows[2].Metric != "GPT-5.3-Codex-Spark" {
		t.Fatalf("unexpected additional primary row: %#v", rows[2])
	}
	if rows[3].Key != "additional_rate_limits.GPT-5.3-Codex-Spark.secondary_window" {
		t.Fatalf("unexpected additional secondary row: %#v", rows[3])
	}
}

func TestParseCodexHeaderQuotaMapsMonthlyWindowMinutes(t *testing.T) {
	output, ok := parseCodexHeaderQuota(http.Header{
		"X-Codex-Primary-Used-Percent":   []string{"33"},
		"X-Codex-Primary-Window-Minutes": []string{"43800"},
		"X-Codex-Primary-Reset-At":       []string{"1782702643"},
	})
	if !ok {
		t.Fatal("expected monthly window to parse")
	}
	rows := NormalizeQuotaRows(output)
	if len(rows) != 1 || rows[0].Window == nil || rows[0].Window.Seconds == nil || *rows[0].Window.Seconds != quotaWindowAverageMonthSeconds || rows[0].Label != "Monthly" {
		t.Fatalf("unexpected monthly rows: %#v", rows)
	}
}

func TestBuildCodexUsageHeaderSnapshotKeepsOnlyQuotaHeaders(t *testing.T) {
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
		AuthType:  "oauth",
		AuthIndex: "codex-auth",
		Provider:  "codex",
		Headers: http.Header{
			"Date":                                      []string{"Mon, 22 Jun 2026 03:10:44 GMT"},
			"Set-Cookie":                                []string{"session=secret"},
			"X-Codex-Credits-Has-Credits":               []string{"False"},
			"X-Codex-Plan-Type":                         []string{"pro"},
			"X-Codex-Primary-Used-Percent":              []string{"", "4", "99"},
			"X-Codex-Primary-Window-Minutes":            []string{"300"},
			"X-Codex-Primary-Reset-At":                  []string{"1782105247"},
			"X-Codex-Bengalfox-Limit-Name":              []string{"GPT-5.3-Codex-Spark"},
			"X-Codex-Bengalfox-Primary-Used-Percent":    []string{"8"},
			"X-Codex-Bengalfox-Primary-Window-Minutes":  []string{"300"},
			"X-Codex-Bengalfox-Primary-Reset-At":        []string{"1782105247"},
			"X-Codex-Bengalfox-Primary-Unrelated-Field": []string{"ignored"},
		},
	})
	if !ok {
		t.Fatal("expected codex header snapshot to build")
	}
	for _, key := range []string{"Date", "Set-Cookie", "X-Codex-Credits-Has-Credits", "X-Codex-Bengalfox-Primary-Unrelated-Field"} {
		if snapshot.Headers.Get(key) != "" {
			t.Fatalf("expected %s to be filtered out, got %#v", key, snapshot.Headers.Values(key))
		}
	}
	if values := snapshot.Headers.Values("X-Codex-Primary-Used-Percent"); len(values) != 1 || values[0] != "4" {
		t.Fatalf("expected first non-empty used percent value to be kept, got %#v", values)
	}
	for _, key := range []string{
		"X-Codex-Plan-Type",
		"X-Codex-Primary-Window-Minutes",
		"X-Codex-Primary-Reset-At",
		"X-Codex-Bengalfox-Limit-Name",
		"X-Codex-Bengalfox-Primary-Used-Percent",
		"X-Codex-Bengalfox-Primary-Window-Minutes",
		"X-Codex-Bengalfox-Primary-Reset-At",
	} {
		if snapshot.Headers.Get(key) == "" {
			t.Fatalf("expected quota header %s to be retained, got %#v", key, snapshot.Headers)
		}
	}
}

func TestParseCodexHeaderQuotaRejectsOverflowWindowMinutes(t *testing.T) {
	overflowToFiveHourMinutes := strconv.FormatInt(300+(math.MaxInt64/2+1), 10)
	output, ok := parseCodexHeaderQuota(http.Header{
		"X-Codex-Primary-Used-Percent":   []string{"33"},
		"X-Codex-Primary-Window-Minutes": []string{overflowToFiveHourMinutes},
		"X-Codex-Primary-Reset-At":       []string{"1782702643"},
	})
	if ok {
		t.Fatalf("expected overflow-sized window minutes to be ignored, got %#v", output)
	}
}

func TestParseCodexHeaderQuotaIgnoresCreditsAndInvalidNumbers(t *testing.T) {
	output, ok := parseCodexHeaderQuota(http.Header{
		"X-Codex-Credits-Has-Credits":    []string{"False"},
		"X-Codex-Primary-Used-Percent":   []string{"bad"},
		"X-Codex-Primary-Window-Minutes": []string{"also-bad"},
	})
	if ok {
		t.Fatalf("expected invalid/incomplete header to be ignored, got %#v", output)
	}
}

func TestParseCodexHeaderQuotaRequiresValidUsedPercentPerWindow(t *testing.T) {
	output, ok := parseCodexHeaderQuota(http.Header{
		"X-Codex-Primary-Window-Minutes":           []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds":      []string{"60"},
		"X-Codex-Secondary-Used-Percent":           []string{"22"},
		"X-Codex-Secondary-Window-Minutes":         []string{"10080"},
		"X-Codex-Secondary-Reset-After-Seconds":    []string{"120"},
		"X-Codex-Bengalfox-Limit-Name":             []string{"GPT-5.3-Codex-Spark"},
		"X-Codex-Bengalfox-Primary-Window-Minutes": []string{"300"},
	})
	if !ok {
		t.Fatal("expected secondary window with valid used percent to parse")
	}
	rows := NormalizeQuotaRows(output)
	if len(rows) != 1 {
		t.Fatalf("expected only windows with valid used percent to parse, got %#v", rows)
	}
	if rows[0].Key != "rate_limit.secondary_window" || rows[0].UsedPercent == nil || *rows[0].UsedPercent != 22 {
		t.Fatalf("unexpected parsed row: %#v", rows[0])
	}
}

func TestParseCodexHeaderQuotaRequiresWindowMinutesAndResetBoundary(t *testing.T) {
	output, ok := parseCodexHeaderQuota(http.Header{
		"X-Codex-Primary-Used-Percent":        []string{"4"},
		"X-Codex-Primary-Reset-After-Seconds": []string{"60"},
		"X-Codex-Secondary-Used-Percent":      []string{"22"},
		"X-Codex-Secondary-Window-Minutes":    []string{"10080"},
	})
	if ok {
		t.Fatalf("expected quota header without complete window/reset data to be ignored, got %#v", output)
	}
}

func TestParseCodexHeaderQuotaIgnoresWindowWithoutUsedPercent(t *testing.T) {
	output, ok := parseCodexHeaderQuota(http.Header{
		"X-Codex-Primary-Window-Minutes":      []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds": []string{"60"},
		"X-Codex-Bengalfox-Limit-Name":        []string{"GPT-5.3-Codex-Spark"},
		"X-Codex-Bengalfox-Reset-At":          []string{"1782115844"},
	})
	if ok {
		t.Fatalf("expected quota header without valid used percent to be ignored, got %#v", output)
	}
}

func TestParseCodexHeaderQuotaAcceptsLowercaseHeaderKeys(t *testing.T) {
	output, ok := parseCodexHeaderQuota(http.Header{
		"x-codex-plan-type":                     []string{"pro"},
		"x-codex-primary-used-percent":          []string{"12"},
		"x-codex-primary-window-minutes":        []string{"300"},
		"x-codex-primary-reset-after-seconds":   []string{"60"},
		"x-codex-secondary-used-percent":        []string{"24"},
		"x-codex-secondary-window-minutes":      []string{"43200"},
		"x-codex-secondary-reset-after-seconds": []string{"120"},
	})
	if !ok {
		t.Fatal("expected lowercase codex headers to parse")
	}
	rows := NormalizeQuotaRows(output)
	if len(rows) != 2 || rows[1].Window == nil || rows[1].Window.Seconds == nil || *rows[1].Window.Seconds != quotaWindowThirtyDaySeconds {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}
