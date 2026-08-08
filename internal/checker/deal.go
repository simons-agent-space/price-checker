package checker

import "sort"

// Window is the number of recent successful price checks the deal detector
// reads to compute a baseline. Three is the minimum: fewer than that and
// we don't know enough to flag a real deal.
const Window = 10

// MinBaseline is the minimum number of successful checks required before the
// detector will flag a deal. Without a baseline, the first check would
// always look like a "good deal" against zero.
const MinBaseline = 3

// Threshold is the fraction below the baseline median that triggers a deal.
// 0.20 means "any price at least 20% below the median is a deal".
const Threshold = 0.20

// IsDeal reports whether currentPrice is at least Threshold below the median
// of the supplied recent prices. Returns (false, 0, nil) when there are
// fewer than MinBaseline entries. median is returned for logging.
func IsDeal(recentPrices []int64, currentPrice int64) (isDeal bool, median int64) {
	if len(recentPrices) < MinBaseline {
		return false, 0
	}
	prices := make([]int64, len(recentPrices))
	copy(prices, recentPrices)
	sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
	median = prices[len(prices)/2]
	cutoff := int64(float64(median) * (1 - Threshold))
	return currentPrice < cutoff, median
}
