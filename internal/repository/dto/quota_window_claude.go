package dto

var claudeQuotaHistoryProviderSpec = QuotaHistoryProviderSpec{
	Provider: "claude",
	Windows: []QuotaWindowSpec{
		{WindowRole: "primary", QuotaKey: "five_hour", Order: 0},
		{WindowRole: "secondary", QuotaKey: "seven_day", Order: 1},
	},
}
