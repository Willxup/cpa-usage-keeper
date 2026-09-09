package test

import (
	"math"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/quota"
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
	if rows[0].Key != "rate_limit.primary_window" || rows[0].Window == nil || rows[0].Window.Seconds == nil || *rows[0].Window.Seconds != quotaWindowFiveHourSeconds {
		t.Fatalf("unexpected primary row: %#v", rows[0])
	}
	subscription := NormalizeSubscription(output)
	if subscription == nil || subscription.Provider != "codex" || subscription.Plan != "pro-20x" {
		t.Fatalf("unexpected subscription: %#v", subscription)
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

func TestBuildCodexUsageHeaderSnapshotCreatesImmutableCacheAndHistoryProjections(t *testing.T) {
	// 固定 observation instant，证明 history 使用真实 UsageEvent 时间而不是 runner 入队时间。
	observedAt := time.Date(2026, 6, 22, 3, 10, 44, 0, time.UTC)
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: observedAt,
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
	// 异步快照不再持有任何 Header map，原始 Cookie/Date 也就无法进入分钟或 history 队列。
	if _, exists := reflect.TypeOf(*snapshot).FieldByName("Headers"); exists {
		t.Fatal("expected immutable structured snapshot to omit http.Header")
	}
	// cache 投影必须保留现有主额度和 Additional 行，且不受 history 只选主额度的规则影响。
	rows := NormalizeQuotaRows(snapshot.CacheOutput)
	if len(rows) != 2 || rows[0].Key != "rate_limit.primary_window" || rows[1].Key != "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window" {
		t.Fatalf("unexpected immutable cache output rows: %#v", rows)
	}
	// history 投影只允许无 group Primary，并保留整数剩余值、窗口秒数和观察时间。
	if len(snapshot.MainQuotaObservations) != 1 {
		t.Fatalf("expected one main quota observation, got %+v", snapshot.MainQuotaObservations)
	}
	observation := snapshot.MainQuotaObservations[0]
	if observation.AuthIndex != "codex-auth" || observation.WindowRole != "primary" || observation.WindowSeconds != 18_000 || observation.RemainingPercent != 96 || !observation.FirstObservedAt.Equal(observedAt) {
		t.Fatalf("unexpected main quota observation: %+v", observation)
	}
	if observation.ResetAtSource != "absolute" {
		t.Fatalf("unexpected main quota window metadata: %+v", observation)
	}
}

func TestBuildCodexUsageHeaderSnapshotSkipsAdditionalActiveLimitHistory(t *testing.T) {
	// 生产 Spark 响应会把当前 Additional 额度重复投影到无 group Primary。
	// cache 只保留带 group 的 Spark 行，主历史也不接受这份投影。
	observedAt := time.Date(2026, 8, 27, 14, 5, 0, 0, time.Local)
	headers := http.Header{
		"x-codex-active-limit":                            []string{"codex_bengalfox"},
		"X-Codex-Primary-Used-Percent":                    []string{"4"},
		"X-Codex-Primary-Window-Minutes":                  []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds":             []string{"7200"},
		"X-Codex-Secondary-Used-Percent":                  []string{"6"},
		"X-Codex-Secondary-Window-Minutes":                []string{"10080"},
		"X-Codex-Secondary-Reset-After-Seconds":           []string{"3600"},
		"X-Codex-Bengalfox-Limit-Name":                    []string{"GPT-5.3-Codex-Spark"},
		"X-Codex-Bengalfox-Primary-Used-Percent":          []string{"4"},
		"X-Codex-Bengalfox-Primary-Window-Minutes":        []string{"300"},
		"X-Codex-Bengalfox-Primary-Reset-After-Seconds":   []string{"7200"},
		"X-Codex-Bengalfox-Secondary-Used-Percent":        []string{"6"},
		"X-Codex-Bengalfox-Secondary-Window-Minutes":      []string{"10080"},
		"X-Codex-Bengalfox-Secondary-Reset-After-Seconds": []string{"3600"},
	}
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
		AuthType: "oauth", AuthIndex: "spark-auth", Provider: "codex",
		ObservedAt: observedAt, Headers: headers,
	})
	if !ok || snapshot == nil {
		t.Fatal("expected Spark header to keep its cache snapshot")
	}
	rows := NormalizeQuotaRows(snapshot.CacheOutput)
	if len(rows) != 2 || rows[0].Key != "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window" || rows[1].Key != "additional_rate_limits.GPT-5.3-Codex-Spark.secondary_window" {
		t.Fatalf("unexpected Spark cache rows: %#v", rows)
	}
	if len(snapshot.MainQuotaObservations) != 0 {
		t.Fatalf("expected Spark active Additional header to create no main history, got %+v", snapshot.MainQuotaObservations)
	}
}

func TestBuildCodexUsageHeaderSnapshotAcceptsKnownMainAndRejectsUnknownActiveLimit(t *testing.T) {
	baseHeaders := http.Header{
		"X-Codex-Primary-Used-Percent":        []string{"22"},
		"X-Codex-Primary-Window-Minutes":      []string{"10080"},
		"X-Codex-Primary-Reset-After-Seconds": []string{"604800"},
	}
	tests := []struct {
		name        string
		activeLimit string
		wantHistory bool
	}{
		{name: "known main", activeLimit: "premium", wantHistory: true},
		{name: "unknown non-empty", activeLimit: "future_pool", wantHistory: false},
		{name: "unknown normalized empty", activeLimit: "___", wantHistory: false},
		{name: "missing compatibility", wantHistory: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := baseHeaders.Clone()
			if test.activeLimit != "" {
				headers.Set("X-Codex-Active-Limit", test.activeLimit)
			}
			snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
				AuthType: "oauth", AuthIndex: "codex-auth", Provider: "codex",
				ObservedAt: time.Date(2026, 8, 27, 14, 5, 0, 0, time.Local), Headers: headers,
			})
			if !ok || snapshot == nil {
				t.Fatal("expected valid cache snapshot")
			}
			if got := len(snapshot.MainQuotaObservations); (got == 1) != test.wantHistory {
				t.Fatalf("unexpected history eligibility for active limit %q: %+v", test.activeLimit, snapshot.MainQuotaObservations)
			}
		})
	}
}

func TestBuildCodexUsageHeaderSnapshotKeepsUnknownMainWindowHistoryOnly(t *testing.T) {
	// 未知 720 分钟窗口不应放宽现有 cache parser，但必须作为合法 Primary 历史 observation 保存。
	observedAt := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: observedAt,
		Headers: http.Header{
			"X-Codex-Primary-Used-Percent":        []string{"10.49"},
			"X-Codex-Primary-Window-Minutes":      []string{"720"},
			"X-Codex-Primary-Reset-After-Seconds": []string{"60"},
		},
	})
	if !ok || snapshot == nil {
		t.Fatal("expected history-only unknown Codex window snapshot")
	}
	// cache 输出保持旧合同：未知窗口不会产生任何 quota row。
	if rows := NormalizeQuotaRows(snapshot.CacheOutput); len(rows) != 0 {
		t.Fatalf("expected unknown window to stay out of cache output, got %#v", rows)
	}
	if len(snapshot.MainQuotaObservations) != 1 {
		t.Fatalf("expected one unknown-window history observation, got %+v", snapshot.MainQuotaObservations)
	}
	observation := snapshot.MainQuotaObservations[0]
	if observation.WindowSeconds != 43_200 || observation.RemainingPercent != 90 || observation.ResetAtSource != "relative" || !observation.ResetAt.Equal(observedAt.Add(time.Minute)) {
		t.Fatalf("unexpected unknown-window history observation: %+v", observation)
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
