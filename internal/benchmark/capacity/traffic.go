package capacity

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type APIKeyProfile struct {
	Index  int     `json:"index"`
	Tier   string  `json:"tier"`
	Weight float64 `json:"weight"`
}

type apiTrafficTier struct {
	name   string
	start  int
	count  int
	weight float64
}

type apiTrafficTopology struct {
	tiers    []apiTrafficTier
	selector weightedSelector
}

func newAPITrafficTopology(profiles []APIKeyProfile) (apiTrafficTopology, error) {
	if len(profiles) == 0 {
		return apiTrafficTopology{}, fmt.Errorf("API key profiles are required")
	}
	var tiers []apiTrafficTier
	for index, profile := range profiles {
		if profile.Weight <= 0 || strings.TrimSpace(profile.Tier) == "" {
			return apiTrafficTopology{}, fmt.Errorf("invalid API key profile %+v", profile)
		}
		if len(tiers) == 0 || tiers[len(tiers)-1].name != profile.Tier {
			tiers = append(tiers, apiTrafficTier{name: profile.Tier, start: index})
		}
		tiers[len(tiers)-1].count++
		tiers[len(tiers)-1].weight += profile.Weight
	}
	weights := make([]float64, len(tiers))
	for index, tier := range tiers {
		weights[index] = tier.weight
	}
	return apiTrafficTopology{tiers: tiers, selector: newWeightedSelector(weights)}, nil
}

func (topology apiTrafficTopology) choose(timestamp time.Time, random *splitMix64) int {
	tierIndex := topology.selector.choose(random.float64())
	tier := topology.tiers[tierIndex]
	duty := 1.0
	period := timestamp.Unix() / int64((6 * time.Hour).Seconds())
	switch strings.ToLower(tier.name) {
	case "medium":
		duty = 0.35
	case "low":
		duty = 0.10
		period = timestamp.Unix() / int64((24 * time.Hour).Seconds())
	}
	active := max(1, int(math.Ceil(float64(tier.count)*duty)))
	if active >= tier.count {
		return tier.start + int(random.next()%uint64(tier.count))
	}
	rotator := splitMix64{state: uint64(period) ^ uint64(tierIndex+1)*0x9e3779b97f4a7c15}
	rotation := int(rotator.next() % uint64(tier.count))
	offset := (rotation + int(random.next()%uint64(active))) % tier.count
	return tier.start + offset
}

type fractionalShare struct {
	index    int
	fraction float64
}

func BuildAPIKeyProfiles(keyCount int, tiers []TrafficTier, seed uint64) ([]APIKeyProfile, error) {
	if keyCount <= 0 {
		return nil, fmt.Errorf("API key count must be positive")
	}
	if len(tiers) == 0 {
		return nil, fmt.Errorf("traffic tiers are required")
	}
	counts, err := allocateTierCounts(keyCount, tiers)
	if err != nil {
		return nil, err
	}
	profiles := make([]APIKeyProfile, 0, keyCount)
	random := splitMix64{state: seed}
	for tierIndex, tier := range tiers {
		for index := 0; index < counts[tierIndex]; index++ {
			// 档内只做正负 10% 的稳定扰动，不能反转大中小档的业务层级。
			jitter := 0.9 + random.float64()*0.2
			profiles = append(profiles, APIKeyProfile{Index: len(profiles) + 1, Tier: tier.Name, Weight: tier.PerKeyWeight * jitter})
		}
	}
	return profiles, nil
}

func allocateTierCounts(total int, tiers []TrafficTier) ([]int, error) {
	shareTotal := 0.0
	counts := make([]int, len(tiers))
	remainders := make([]fractionalShare, len(tiers))
	allocated := 0
	for index, tier := range tiers {
		if tier.KeyShare <= 0 || tier.PerKeyWeight <= 0 {
			return nil, fmt.Errorf("traffic tier %q must have positive share and weight", tier.Name)
		}
		shareTotal += tier.KeyShare
		exact := float64(total) * tier.KeyShare
		counts[index] = int(math.Floor(exact))
		allocated += counts[index]
		remainders[index] = fractionalShare{index: index, fraction: exact - float64(counts[index])}
	}
	if math.Abs(shareTotal-1) > 1e-9 {
		return nil, fmt.Errorf("traffic tier key shares sum to %.12f, want 1", shareTotal)
	}
	sort.SliceStable(remainders, func(left, right int) bool {
		return remainders[left].fraction > remainders[right].fraction
	})
	for index := 0; index < total-allocated; index++ {
		counts[remainders[index%len(remainders)].index]++
	}
	return counts, nil
}

func AllocateEvents(total int64, profiles []APIKeyProfile) ([]int64, error) {
	if total < 0 {
		return nil, fmt.Errorf("event total cannot be negative")
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("API key profiles are required")
	}
	weightTotal := 0.0
	for _, profile := range profiles {
		if profile.Weight <= 0 {
			return nil, fmt.Errorf("API key %d has non-positive weight", profile.Index)
		}
		weightTotal += profile.Weight
	}
	counts := make([]int64, len(profiles))
	remainders := make([]fractionalShare, len(profiles))
	allocated := int64(0)
	for index, profile := range profiles {
		exact := float64(total) * profile.Weight / weightTotal
		counts[index] = int64(math.Floor(exact))
		allocated += counts[index]
		remainders[index] = fractionalShare{index: index, fraction: exact - float64(counts[index])}
	}
	sort.SliceStable(remainders, func(left, right int) bool {
		return remainders[left].fraction > remainders[right].fraction
	})
	for index := int64(0); index < total-allocated; index++ {
		counts[remainders[index%int64(len(remainders))].index]++
	}
	return counts, nil
}

type splitMix64 struct {
	state uint64
}

func (r *splitMix64) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	value := r.state
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func (r *splitMix64) float64() float64 {
	return float64(r.next()>>11) / (1 << 53)
}
