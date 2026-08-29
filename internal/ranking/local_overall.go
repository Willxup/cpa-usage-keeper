package ranking

import (
	"math"
	"sort"
)

const (
	localOverallGeometricEpsilon = 0.1
	localOverallShrinkagePrior   = 100.0
	localOverallScoreScale       = 100.0
	localOverallUtilityScale     = 1_000_000.0
)

func localOverallEligible(row localRankingPopulationRow) bool {
	outcomes := row.SuccessCount + row.FailureCount
	return row.RequestCount > 0 && outcomes > 0 && row.TotalTokens > 0 && row.InputTokens > 0 &&
		row.TTFTSampleCount > 0 && row.TTFTSumMS > 0 && row.LatencySampleCount > 0 && row.LatencySumMS > 0 &&
		row.Peak5MRequestCount > 0 && row.Peak5MTotalTokens > 0
}

func scoreLocalOverallPopulation(rows []localRankingPopulationRow) []int64 {
	if len(rows) == 0 {
		return []int64{}
	}
	totalTokens := make([]float64, len(rows))
	requests := make([]float64, len(rows))
	cacheRates := make([]float64, len(rows))
	ttft := make([]float64, len(rows))
	latency := make([]float64, len(rows))
	peakTokens := make([]float64, len(rows))
	peakRequests := make([]float64, len(rows))
	cacheSamples := make([]int64, len(rows))
	ttftSamples := make([]int64, len(rows))
	latencySamples := make([]int64, len(rows))
	for index, row := range rows {
		totalTokens[index] = math.Log1p(float64(row.TotalTokens))
		requests[index] = math.Log1p(float64(row.RequestCount))
		cacheRates[index] = float64(scaledRatio(row.CacheReadTokens, row.InputTokens, 1_000_000))
		ttft[index] = float64(scaledRatio(row.TTFTSumMS, row.TTFTSampleCount, 1_000))
		latency[index] = float64(scaledRatio(row.LatencySumMS, row.LatencySampleCount, 1_000))
		peakTokens[index] = math.Log1p(float64(row.Peak5MTotalTokens) / 5)
		peakRequests[index] = math.Log1p(float64(row.Peak5MRequestCount) / 5)
		cacheSamples[index] = row.RequestCount
		ttftSamples[index] = row.TTFTSampleCount
		latencySamples[index] = row.LatencySampleCount
	}

	tokenUtility := localRobustUtilities(totalTokens, true)
	requestUtility := localRobustUtilities(requests, true)
	cacheUtility := localShrinkUtilitiesToNeutral(localRobustUtilities(cacheRates, true), cacheSamples)
	ttftUtility := localShrinkUtilitiesToNeutral(localRobustUtilities(ttft, false), ttftSamples)
	latencyUtility := localShrinkUtilitiesToNeutral(localRobustUtilities(latency, false), latencySamples)
	peakTokenUtility := localRobustUtilities(peakTokens, true)
	peakRequestUtility := localRobustUtilities(peakRequests, true)

	scores := make([]int64, len(rows))
	for index, row := range rows {
		workload := (20*tokenUtility[index] + 10*requestUtility[index]) / 30
		reliability := localWilsonLowerBound(row.SuccessCount, row.FailureCount)
		response := (15*ttftUtility[index] + 10*latencyUtility[index]) / 25
		cache := cacheUtility[index]
		peak := (3*peakTokenUtility[index] + 2*peakRequestUtility[index]) / 5
		scores[index] = localOverallScore([5]float64{workload, reliability, response, cache, peak})
	}
	return scores
}

func localOverallScore(utilities [5]float64) int64 {
	weights := [5]float64{30, 30, 25, 10, 5}
	weightedLog := 0.0
	weightTotal := 0.0
	for index, utility := range utilities {
		// Community V2 先把每个维度四舍五入为 PPM，再进入几何平均。
		quantized := math.Round(localClampUnit(utility)*localOverallUtilityScale) / localOverallUtilityScale
		stabilized := localOverallGeometricEpsilon + (1-localOverallGeometricEpsilon)*quantized
		weightedLog += weights[index] * math.Log(stabilized)
		weightTotal += weights[index]
	}
	geometric := math.Exp(weightedLog / weightTotal)
	normalized := (geometric - localOverallGeometricEpsilon) / (1 - localOverallGeometricEpsilon)
	return int64(math.Round(localClampUnit(normalized) * localOverallScoreScale))
}

func localShrinkUtilitiesToNeutral(utilities []float64, samples []int64) []float64 {
	result := make([]float64, len(utilities))
	for index, utility := range utilities {
		n := math.Max(0, float64(samples[index]))
		weight := n / (n + localOverallShrinkagePrior)
		result[index] = 0.5 + weight*(utility-0.5)
	}
	return result
}

func localRobustUtilities(values []float64, higherIsBetter bool) []float64 {
	center := localMedian(values)
	deviations := make([]float64, len(values))
	for index, value := range values {
		deviations[index] = math.Abs(value - center)
	}
	scale := 1.4826 * localMedian(deviations)
	if scale <= 1e-12 {
		return localSharedPercentileUtilities(values, higherIsBetter)
	}
	result := make([]float64, len(values))
	for index, value := range values {
		z := (value - center) / scale
		if !higherIsBetter {
			z = -z
		}
		z = math.Max(-8, math.Min(8, z))
		result[index] = localClampUnit(0.5 * (1 + math.Erf(z/math.Sqrt2)))
	}
	return result
}

func localSharedPercentileUtilities(values []float64, higherIsBetter bool) []float64 {
	type indexedValue struct {
		index int
		value float64
	}
	ordered := make([]indexedValue, len(values))
	for index, value := range values {
		ordered[index] = indexedValue{index: index, value: value}
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].value != ordered[right].value {
			return ordered[left].value < ordered[right].value
		}
		return ordered[left].index < ordered[right].index
	})
	result := make([]float64, len(values))
	for start := 0; start < len(ordered); {
		end := start + 1
		for end < len(ordered) && ordered[end].value == ordered[start].value {
			end++
		}
		percentile := float64(start+end) / (2 * float64(len(ordered)))
		if !higherIsBetter {
			percentile = 1 - percentile
		}
		for index := start; index < end; index++ {
			result[ordered[index].index] = percentile
		}
		start = end
	}
	return result
}

func localMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[middle]
	}
	return (ordered[middle-1] + ordered[middle]) / 2
}

func localWilsonLowerBound(successes, failures int64) float64 {
	n := float64(successes) + float64(failures)
	if n <= 0 {
		return 0
	}
	p := float64(successes) / n
	z := 1.959963984540054
	zSquared := z * z
	center := p + zSquared/(2*n)
	margin := z * math.Sqrt((p*(1-p)+zSquared/(4*n))/n)
	return localClampUnit((center - margin) / (1 + zSquared/n))
}

func localClampUnit(value float64) float64 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value
}

func localOverallScoreExplanation() *ScoreExplanation {
	return &ScoreExplanation{
		Version: 2,
		Texts: map[string]string{
			"en":    "Local overall score combines workload 30% (tokens 20%, requests 10%), reliability 30% (95% Wilson lower bound), response 25% (TTFT 15%, latency 10%), cache efficiency 10%, and peak capacity 5% (TPM 3%, RPM 2%). It is normalized only among API keys in this Keeper and period, and is shown on a 0–100 scale.",
			"zh":    "本地综合分由使用规模 30%（Token 20%、请求数 10%）、可靠性 30%（成功率的 95% Wilson 下界）、响应体验 25%（首字延迟 15%、总延迟 10%）、缓存效率 10% 和峰值能力 5%（TPM 3%、RPM 2%）组成。分数仅在当前 Keeper、当前周期的 API Key 之间归一化，范围为 0–100。",
			"zh-TW": "本地綜合分由使用規模 30%（Token 20%、請求數 10%）、可靠性 30%（成功率的 95% Wilson 下界）、回應體驗 25%（首字延遲 15%、總延遲 10%）、快取效率 10% 和峰值能力 5%（TPM 3%、RPM 2%）組成。分數僅在目前 Keeper、目前週期的 API Key 之間正規化，範圍為 0–100。",
		},
	}
}
