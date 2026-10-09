package capacity_test

import "cpa-usage-keeper/internal/benchmark/capacity"

func capacityTestTrafficTiers() []capacity.TrafficTier {
	return []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}
}
