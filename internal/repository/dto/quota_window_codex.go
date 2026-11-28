package dto

var codexQuotaHistoryProviderSpec = QuotaHistoryProviderSpec{
	Provider: "codex",
	Windows: []QuotaWindowSpec{
		{WindowRole: "primary", QuotaKey: "rate_limit.primary_window", Order: 0},
		{WindowRole: "secondary", QuotaKey: "rate_limit.secondary_window", Order: 1},
	},
}
