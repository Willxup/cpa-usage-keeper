package percentile

import (
	"math"
	"slices"
)

const (
	partitionWorkMultiplier = 4
	smallRangeSortThreshold = 32
)

// NearestRank 原地选择最近秩百分位；累计分区工作超过 4n 前回退排序当前活动区间。
func NearestRank(values []int64, target float64) int64 {
	if len(values) == 0 {
		return 0
	}

	index := nearestRankIndex(len(values), target)
	selectIndex(values, index)
	return values[index]
}

func nearestRankIndex(length int, target float64) int {
	if math.IsNaN(target) || target <= 0 {
		return 0
	}
	if target >= 1 {
		return length - 1
	}
	return int(math.Ceil(target*float64(length))) - 1
}

func selectIndex(values []int64, targetIndex int) {
	left, right := 0, len(values)
	remainingWork := partitionWorkBudget(len(values))

	for right-left > smallRangeSortThreshold {
		activeLength := right - left
		// 下一轮无法在预算内完成时直接排序，避免先支付一轮注定越界的分区成本。
		if activeLength > remainingWork {
			slices.Sort(values[left:right])
			return
		}
		remainingWork -= activeLength

		lowerEnd, upperStart := partitionThreeWay(values, left, right, medianOfThreePivot(values, left, right))
		switch {
		case targetIndex < lowerEnd:
			right = lowerEnd
		case targetIndex >= upperStart:
			left = upperStart
		default:
			return
		}
	}

	slices.Sort(values[left:right])
}

func partitionWorkBudget(length int) int {
	maxInt := int(^uint(0) >> 1)
	if length > maxInt/partitionWorkMultiplier {
		return maxInt
	}
	return length * partitionWorkMultiplier
}

func medianOfThreePivot(values []int64, left, right int) int64 {
	first := values[left]
	middle := values[left+(right-left)/2]
	last := values[right-1]
	if first > middle {
		first, middle = middle, first
	}
	if middle > last {
		middle, last = last, middle
	}
	if first > middle {
		middle = first
	}
	return middle
}

// partitionThreeWay 把活动区间分成小于、等于和大于 pivot 的三段，并返回等值段边界。
func partitionThreeWay(values []int64, left, right int, pivot int64) (int, int) {
	lowerEnd, index, upperStart := left, left, right
	for index < upperStart {
		switch {
		case values[index] < pivot:
			values[lowerEnd], values[index] = values[index], values[lowerEnd]
			lowerEnd++
			index++
		case values[index] > pivot:
			upperStart--
			values[index], values[upperStart] = values[upperStart], values[index]
		default:
			index++
		}
	}
	return lowerEnd, upperStart
}
