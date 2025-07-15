package metrics

import (
	"math"
	"sort"
)

// AdvancedCalculator provides advanced statistical calculations
type AdvancedCalculator struct{}

// NewAdvancedCalculator creates a new advanced calculator
func NewAdvancedCalculator() *AdvancedCalculator {
	return &AdvancedCalculator{}
}

// CalculatePercentile calculates the percentile value from a sorted slice
func (ac *AdvancedCalculator) CalculatePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	index := (percentile / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// CalculateVariance calculates the variance of values
func (ac *AdvancedCalculator) CalculateVariance(values []float64, mean float64) float64 {
	if len(values) <= 1 {
		return 0
	}

	var sum float64
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}

	return sum / float64(len(values)-1)
}

// CalculateStdDev calculates standard deviation
func (ac *AdvancedCalculator) CalculateStdDev(variance float64) float64 {
	return math.Sqrt(variance)
}

// CalculateSkewness calculates the skewness of the distribution
func (ac *AdvancedCalculator) CalculateSkewness(values []float64, mean, stdDev float64) float64 {
	if len(values) < 3 || stdDev == 0 {
		return 0
	}

	n := float64(len(values))
	var sum float64

	for _, v := range values {
		z := (v - mean) / stdDev
		sum += z * z * z
	}

	return (n / ((n - 1) * (n - 2))) * sum
}

// CalculateKurtosis calculates the kurtosis of the distribution
func (ac *AdvancedCalculator) CalculateKurtosis(values []float64, mean, stdDev float64) float64 {
	if len(values) < 4 || stdDev == 0 {
		return 0
	}

	n := float64(len(values))
	var sum float64

	for _, v := range values {
		z := (v - mean) / stdDev
		sum += z * z * z * z
	}

	factor1 := (n * (n + 1)) / ((n - 1) * (n - 2) * (n - 3))
	factor2 := (3 * (n - 1) * (n - 1)) / ((n - 2) * (n - 3))

	return factor1*sum - factor2
}

// CalculateIQR calculates the interquartile range
func (ac *AdvancedCalculator) CalculateIQR(values []float64) float64 {
	if len(values) < 4 {
		return 0
	}

	q1 := ac.CalculatePercentile(values, 25)
	q3 := ac.CalculatePercentile(values, 75)

	return q3 - q1
}

// CalculateMAD calculates the median absolute deviation
func (ac *AdvancedCalculator) CalculateMAD(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	median := ac.CalculatePercentile(values, 50)

	deviations := make([]float64, len(values))
	for i, v := range values {
		deviations[i] = math.Abs(v - median)
	}

	return ac.CalculatePercentile(deviations, 50)
}

// CalculateCoeffVar calculates the coefficient of variation
func (ac *AdvancedCalculator) CalculateCoeffVar(mean, stdDev float64) float64 {
	if mean == 0 {
		return 0
	}
	return (stdDev / math.Abs(mean)) * 100
}

// DetectOutliers detects outliers using the IQR method
func (ac *AdvancedCalculator) DetectOutliers(values []float64) ([]float64, []int) {
	if len(values) < 4 {
		return []float64{}, []int{}
	}

	q1 := ac.CalculatePercentile(values, 25)
	q3 := ac.CalculatePercentile(values, 75)
	iqr := q3 - q1

	lowerBound := q1 - 1.5*iqr
	upperBound := q3 + 1.5*iqr

	var outliers []float64
	var indices []int

	for i, v := range values {
		if v < lowerBound || v > upperBound {
			outliers = append(outliers, v)
			indices = append(indices, i)
		}
	}

	return outliers, indices
}

// CalculateJitter calculates jitter (variance in latency)
func (ac *AdvancedCalculator) CalculateJitter(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	var jitterSum float64
	for i := 1; i < len(values); i++ {
		diff := math.Abs(values[i] - values[i-1])
		jitterSum += diff
	}

	return jitterSum / float64(len(values)-1)
}
