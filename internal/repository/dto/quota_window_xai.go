package dto

var xaiQuotaHistoryProviderSpec = QuotaHistoryProviderSpec{
	Provider: "xai",
	Windows: []QuotaWindowSpec{
		{WindowRole: "weekly", QuotaKey: "billing.weekly", Order: 0},
	},
}
