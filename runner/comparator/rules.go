package comparator

import (
	"math/big"
	"regexp"
	"strings"
)

// ComparisonRuleKind enumerates the ways a difference can be declared expected.
type ComparisonRuleKind string

const (
	// RuleIgnore drops a JSON path from comparison entirely.
	RuleIgnore ComparisonRuleKind = "ignore"
	// RuleNumericTolerance treats two hex quantities as equal when they are
	// within an absolute and/or relative tolerance of each other.
	RuleNumericTolerance ComparisonRuleKind = "numeric_tolerance"
	// RuleErrorCodeOnly compares only the JSON-RPC error code, ignoring the
	// human-readable message, when both responses are errors.
	RuleErrorCodeOnly ComparisonRuleKind = "error_code_only"
	// RuleErrorPresenceOnly treats any two error responses as equal.
	RuleErrorPresenceOnly ComparisonRuleKind = "error_presence_only"
)

// ComparisonRule declares that a particular difference is expected and should
// not be reported as a finding. A rule with an empty Method applies to every
// method; an empty Path applies to the whole response value.
type ComparisonRule struct {
	Method string             `json:"method,omitempty" yaml:"method,omitempty"`
	Path   string             `json:"path,omitempty" yaml:"path,omitempty"`
	Kind   ComparisonRuleKind `json:"kind" yaml:"kind"`
	Abs    float64            `json:"abs,omitempty" yaml:"abs,omitempty"`
	Rel    float64            `json:"rel,omitempty" yaml:"rel,omitempty"`
}

// ValidRuleKind reports whether kind is one of the supported rule kinds.
func ValidRuleKind(kind ComparisonRuleKind) bool {
	switch kind {
	case RuleIgnore, RuleNumericTolerance, RuleErrorCodeOnly, RuleErrorPresenceOnly:
		return true
	default:
		return false
	}
}

// defaultEstimateGasRelTolerance is applied to eth_estimateGas unless the
// config already declares a numeric_tolerance rule for it: gas estimates that
// differ by no more than 10% are treated as equal.
const defaultEstimateGasRelTolerance = 0.10

// diffContext carries the method under comparison and its compiled rules
// through the recursive diff so rules can be applied by path.
type diffContext struct {
	method string
	rules  *ruleSet
}

type compiledRule struct {
	rule ComparisonRule
	re   *regexp.Regexp
}

type ruleSet struct {
	ignores           []compiledRule
	tolerances        []compiledRule
	errorCodeOnly     bool
	errorPresenceOnly bool
}

// newDiffContext filters the config rules down to those that apply to method
// and injects the built-in eth_estimateGas tolerance when none is configured.
func newDiffContext(method string, rules []ComparisonRule) *diffContext {
	applicable := make([]ComparisonRule, 0, len(rules)+1)
	hasEstimateGasTolerance := false
	for _, r := range rules {
		if r.Method != "" && r.Method != method {
			continue
		}
		applicable = append(applicable, r)
		if method == "eth_estimateGas" && r.Kind == RuleNumericTolerance {
			hasEstimateGasTolerance = true
		}
	}
	if method == "eth_estimateGas" && !hasEstimateGasTolerance {
		applicable = append(applicable, ComparisonRule{
			Method: "eth_estimateGas",
			Path:   "result",
			Kind:   RuleNumericTolerance,
			Rel:    defaultEstimateGasRelTolerance,
		})
	}
	return &diffContext{method: method, rules: compileRuleSet(applicable)}
}

func compileRuleSet(rules []ComparisonRule) *ruleSet {
	rs := &ruleSet{}
	for _, r := range rules {
		switch r.Kind {
		case RuleIgnore:
			rs.ignores = append(rs.ignores, compiledRule{rule: r, re: compileRulePath(r.Path)})
		case RuleNumericTolerance:
			rs.tolerances = append(rs.tolerances, compiledRule{rule: r, re: compileRulePath(r.Path)})
		case RuleErrorCodeOnly:
			rs.errorCodeOnly = true
		case RuleErrorPresenceOnly:
			rs.errorPresenceOnly = true
		}
	}
	return rs
}

// compileRulePath turns a rule path into an anchored regexp. A trailing or
// embedded "[*]" matches any array index, so "result.transactions[*].v"
// matches "result.transactions[3].v".
func compileRulePath(path string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(path)
	quoted = strings.ReplaceAll(quoted, `\[\*\]`, `\[[0-9]+\]`)
	return regexp.MustCompile("^" + quoted + "$")
}

func (rs *ruleSet) matchesIgnore(path string) bool {
	for _, c := range rs.ignores {
		if c.re != nil && c.re.MatchString(path) {
			return true
		}
	}
	return false
}

func (rs *ruleSet) toleranceFor(path string) (ComparisonRule, bool) {
	for _, c := range rs.tolerances {
		if c.re != nil && c.re.MatchString(path) {
			return c.rule, true
		}
	}
	return ComparisonRule{}, false
}

// withinTolerance reports whether the rule applies to the two hex quantities
// (applicable) and, if so, whether they are equal within tolerance (equal).
func withinTolerance(s1, s2 string, rule ComparisonRule) (applicable, equal bool) {
	a, ok1 := parseHexBig(s1)
	b, ok2 := parseHexBig(s2)
	if !ok1 || !ok2 {
		return false, false
	}

	diff := new(big.Int).Abs(new(big.Int).Sub(a, b))

	if rule.Abs > 0 {
		absLimit := new(big.Int)
		big.NewFloat(rule.Abs).Int(absLimit)
		if diff.Cmp(absLimit) <= 0 {
			return true, true
		}
	}

	if rule.Rel > 0 {
		absA := new(big.Float).SetInt(new(big.Int).Abs(a))
		absB := new(big.Float).SetInt(new(big.Int).Abs(b))
		max := absA
		if absB.Cmp(absA) > 0 {
			max = absB
		}
		if max.Sign() == 0 {
			return true, true
		}
		ratio := new(big.Float).Quo(new(big.Float).SetInt(diff), max)
		if ratio.Cmp(big.NewFloat(rule.Rel)) <= 0 {
			return true, true
		}
	}

	return true, false
}

// parseHexBig parses a 0x-prefixed hex quantity into a big.Int. "0x" parses
// to zero. It returns false for non-hex strings.
func parseHexBig(s string) (*big.Int, bool) {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return nil, false
	}
	digits := s[2:]
	if digits == "" {
		return big.NewInt(0), true
	}
	v, ok := new(big.Int).SetString(digits, 16)
	if !ok {
		return nil, false
	}
	return v, true
}

// errorCode extracts the numeric JSON-RPC error code from an error value.
func errorCode(errVal interface{}) (int, bool) {
	m, ok := errVal.(map[string]interface{})
	if !ok {
		return 0, false
	}
	code, ok := m["code"].(float64)
	if !ok {
		return 0, false
	}
	return int(code), true
}

// classifyError buckets an error response into an environment/capability class
// so it can be filtered as configuration rather than a correctness finding.
// It returns "" for ordinary execution errors and non-error responses.
func classifyError(resp map[string]interface{}) string {
	e, ok := resp["error"].(map[string]interface{})
	if !ok {
		return ""
	}
	code := 0
	if f, ok := e["code"].(float64); ok {
		code = int(f)
	}
	msg, _ := e["message"].(string)
	lower := strings.ToLower(msg)

	switch code {
	case -32601, -32600:
		return "namespace_disabled"
	case -32002:
		return "no_state"
	case -32602:
		if strings.Contains(lower, "range") || strings.Contains(lower, "logs") || strings.Contains(lower, "limit") {
			return "range_cap"
		}
	}
	return ""
}
