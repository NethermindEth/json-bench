package engine

import (
	"math"
	"sort"

	"github.com/jsonrpc-bench/runner/types"
)

// minComparisonSamples is the smallest per-client sample count worth testing.
// Below it the normal approximation behind the U statistic does not hold and a
// p-value would be a decoration.
const minComparisonSamples = 20

// CompareClients runs a two-sided Mann-Whitney U test on each method's latency
// for every pair of clients, from the samples the run retained.
//
// The rank-sum test is the right tool here: latency distributions are skewed
// and heavy-tailed, so a test assuming normality would answer a question about
// a distribution the data does not have.
//
// A caution that belongs with the output rather than in a footnote: at
// benchmark sample sizes almost any difference is significant. A run of 30k
// requests per client will report p < 0.001 for a median difference of a tenth
// of a millisecond. The effect size is the part worth reading.
func (a *Accumulator) CompareClients() []types.MethodComparison {
	a.mu.Lock()
	byClient := make(map[string]map[string][]float64, len(a.clients))
	for name, client := range a.clients {
		methods := make(map[string][]float64)
		for key, g := range client.keyed {
			methods[key.method] = append(methods[key.method], g.phases[PhaseDuration]...)
		}
		byClient[name] = methods
	}
	a.mu.Unlock()

	if len(byClient) < 2 {
		return nil
	}

	names := make([]string, 0, len(byClient))
	for name := range byClient {
		names = append(names, name)
	}
	sort.Strings(names)

	methods := make(map[string]struct{})
	for _, client := range byClient {
		for method := range client {
			methods[method] = struct{}{}
		}
	}
	methodNames := make([]string, 0, len(methods))
	for method := range methods {
		methodNames = append(methodNames, method)
	}
	sort.Strings(methodNames)

	var out []types.MethodComparison
	for _, method := range methodNames {
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				a, b := byClient[names[i]][method], byClient[names[j]][method]
				if len(a) < minComparisonSamples || len(b) < minComparisonSamples {
					continue
				}
				out = append(out, compareSamples(method, names[i], names[j], a, b))
			}
		}
	}
	return out
}

func compareSamples(method, clientA, clientB string, a, b []float64) types.MethodComparison {
	sortedA := append([]float64(nil), a...)
	sortedB := append([]float64(nil), b...)
	sort.Float64s(sortedA)
	sort.Float64s(sortedB)

	medianA := calc.CalculatePercentile(sortedA, 50)
	medianB := calc.CalculatePercentile(sortedB, 50)

	u, z, p := mannWhitney(a, b)

	comparison := types.MethodComparison{
		Method:        method,
		ClientA:       clientA,
		ClientB:       clientB,
		CountA:        len(a),
		CountB:        len(b),
		MedianAMs:     medianA,
		MedianBMs:     medianB,
		P99AMs:        calc.CalculatePercentile(sortedA, 99),
		P99BMs:        calc.CalculatePercentile(sortedB, 99),
		MedianShiftMs: medianB - medianA,
		U:             u,
		Z:             z,
		PValue:        p,
	}
	if medianA > 0 {
		comparison.MedianShiftPercent = (medianB - medianA) / medianA * 100
	}
	// Named only when the difference is unlikely to be noise. The magnitude is
	// still the reader's judgement, which is why the shift is reported too.
	if p < 0.05 {
		switch {
		case medianA < medianB:
			comparison.Faster = clientA
		case medianB < medianA:
			comparison.Faster = clientB
		}
	}
	return comparison
}

// mannWhitney returns the U statistic for the first sample, the tie-corrected
// normal-approximation z, and the two-sided p-value.
func mannWhitney(a, b []float64) (u, z, p float64) {
	n1, n2 := len(a), len(b)
	if n1 == 0 || n2 == 0 {
		return 0, 0, 1
	}

	type observation struct {
		value float64
		first bool
	}
	combined := make([]observation, 0, n1+n2)
	for _, v := range a {
		combined = append(combined, observation{v, true})
	}
	for _, v := range b {
		combined = append(combined, observation{v, false})
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].value < combined[j].value })

	// Average ranks within a tie group, and record the group sizes: latency
	// measurements tie often once rounded, and ignoring ties inflates the
	// variance and understates significance.
	var rankSumA float64
	var tieCorrection float64
	for i := 0; i < len(combined); {
		j := i
		for j < len(combined) && combined[j].value == combined[i].value {
			j++
		}
		groupSize := j - i
		averageRank := float64(i+j+1) / 2
		for k := i; k < j; k++ {
			if combined[k].first {
				rankSumA += averageRank
			}
		}
		if groupSize > 1 {
			t := float64(groupSize)
			tieCorrection += t*t*t - t
		}
		i = j
	}

	f1, f2 := float64(n1), float64(n2)
	n := f1 + f2
	u = rankSumA - f1*(f1+1)/2
	mean := f1 * f2 / 2

	variance := f1 * f2 / 12 * ((n + 1) - tieCorrection/(n*(n-1)))
	if variance <= 0 {
		return u, 0, 1
	}

	z = (u - mean) / math.Sqrt(variance)
	return u, z, twoSidedP(z)
}

// twoSidedP is the two-tailed normal tail probability for z.
func twoSidedP(z float64) float64 {
	p := math.Erfc(math.Abs(z) / math.Sqrt2)
	if p > 1 {
		return 1
	}
	if p < 0 {
		return 0
	}
	return p
}
