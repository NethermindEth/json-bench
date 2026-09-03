package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/internal/promsink"
)

const dashboardPath = "../../metrics/dashboards/benchmark-dashboard.json"

var (
	metricPattern   = regexp.MustCompile(`bench_[a-z0-9_]+`)
	matcherPattern  = regexp.MustCompile(`\{([^}]*)\}`)
	labelPattern    = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:=~|!~|!=|=)`)
	groupingPattern = regexp.MustCompile(`\bby\s*\(([^)]*)\)`)
	legendPattern   = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)
)

// seriesLabels reads the committed capture into metric name -> label names.
func seriesLabels(t *testing.T) map[string]map[string]bool {
	t.Helper()

	data, err := os.ReadFile(benchGoldenPath)
	require.NoError(t, err)
	_, keys, err := promsink.ParseGolden(data)
	require.NoError(t, err)

	out := make(map[string]map[string]bool, len(keys))
	for _, key := range keys {
		name, labels, _ := strings.Cut(key, "{")
		set := make(map[string]bool)
		for _, label := range strings.Split(strings.TrimSuffix(labels, "}"), ",") {
			if label != "" {
				set[label] = true
			}
		}
		out[name] = set
	}
	return out
}

type panelQuery struct {
	panel  string
	expr   string
	legend string
}

func dashboardQueries(t *testing.T) []panelQuery {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(dashboardPath))
	require.NoError(t, err)

	var dashboard struct {
		Panels []struct {
			Title   string `json:"title"`
			Type    string `json:"type"`
			Targets []struct {
				Expr         string `json:"expr"`
				LegendFormat string `json:"legendFormat"`
			} `json:"targets"`
		} `json:"panels"`
	}
	require.NoError(t, json.Unmarshal(data, &dashboard))

	var out []panelQuery
	for _, panel := range dashboard.Panels {
		for _, target := range panel.Targets {
			if strings.TrimSpace(target.Expr) == "" {
				continue
			}
			out = append(out, panelQuery{panel: panel.Title, expr: target.Expr, legend: target.LegendFormat})
		}
	}
	require.NotEmpty(t, out)
	return out
}

// expandStatVariable substitutes the dashboard's stat picker, which the panels
// interpolate into metric names.
func expandStatVariable(expr string) []string {
	if !strings.Contains(expr, "$quantile_stat") {
		return []string{expr}
	}
	out := make([]string, 0, 9)
	for _, stat := range []string{"min", "max", "avg", "med", "p75", "p90", "p95", "p99", "p999"} {
		out = append(out, strings.ReplaceAll(expr, "$quantile_stat", stat))
	}
	return out
}

// A panel querying a metric the engine does not emit renders nothing at all —
// no error, just an empty graph. That is exactly how four panels in the k6
// dashboard sat broken, so the dashboard is checked against the emitted set
// rather than trusted.
func TestDashboardQueriesOnlyEmittedMetrics(t *testing.T) {
	emitted := seriesLabels(t)

	for _, query := range dashboardQueries(t) {
		for _, expr := range expandStatVariable(query.expr) {
			for _, name := range metricPattern.FindAllString(expr, -1) {
				assert.Contains(t, emitted, name,
					"panel %q queries %s, which the engine does not emit", query.panel, name)
			}
		}
	}
}

func TestDashboardUsesOnlyEmittedLabels(t *testing.T) {
	emitted := seriesLabels(t)

	for _, query := range dashboardQueries(t) {
		for _, expr := range expandStatVariable(query.expr) {
			names := metricPattern.FindAllString(expr, -1)
			if len(names) == 0 {
				continue
			}

			carries := func(label string) bool {
				for _, name := range names {
					if emitted[name][label] {
						return true
					}
				}
				return false
			}

			for _, label := range usedLabels(expr) {
				assert.True(t, carries(label),
					"panel %q filters or groups by %q, which none of %v carries", query.panel, label, names)
			}
			for _, label := range legendPattern.FindAllStringSubmatch(query.legend, -1) {
				assert.True(t, carries(label[1]),
					"panel %q renders {{%s}} in its legend, which none of %v carries", query.panel, label[1], names)
			}
		}
	}
}

func usedLabels(expr string) []string {
	seen := map[string]bool{}

	for _, matcher := range matcherPattern.FindAllStringSubmatch(expr, -1) {
		for _, label := range labelPattern.FindAllStringSubmatch(matcher[1], -1) {
			seen[label[1]] = true
		}
	}
	for _, grouping := range groupingPattern.FindAllStringSubmatch(expr, -1) {
		for _, label := range strings.Split(grouping[1], ",") {
			if label = strings.TrimSpace(label); label != "" {
				seen[label] = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// The k6 dashboard filtered on a label k6 never emitted, so those panels drew
// nothing. The replacement is the outcome label, which is emitted and is
// RPC-aware.
func TestDashboardDoesNotUseTheDeadK6Labels(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(dashboardPath))
	require.NoError(t, err)
	raw := string(data)

	for _, dead := range []string{"expected_response", "k6_checks", "k6_vus", "k6_http_req", "k6_iteration"} {
		assert.NotContains(t, raw, dead)
	}
}

func TestDashboardIdentity(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(dashboardPath))
	require.NoError(t, err)

	var live struct {
		UID   string `json:"uid"`
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal(data, &live))

	archived, err := os.ReadFile(filepath.Join("..", "..", "metrics", "dashboards", "archive", "k6-dashboard.json"))
	require.NoError(t, err)

	var old struct {
		UID   string `json:"uid"`
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal(archived, &old))

	assert.NotEqual(t, old.UID, live.UID, "the ported dashboard needs its own uid")
	// The archived copy keeps the uid it always had, so links and forks of it
	// still resolve after the port.
	assert.Equal(t, "ccbb2351-2ae2-462f-ae0e-f2c893ad1028", old.UID)
	assert.Contains(t, old.Title, "archived")
}

const metricsDocPath = "../../metrics/METRICS.md"

// A metrics reference nobody can trust is worse than none, and documentation
// drifts silently in a way code does not. Every emitted family must be named in
// the doc, and the doc must not promise a family that is not emitted.
func TestMetricsDocCoversEveryFamily(t *testing.T) {
	emitted := seriesLabels(t)

	doc, err := os.ReadFile(filepath.Clean(metricsDocPath))
	require.NoError(t, err)
	text := string(doc)

	families := map[string]bool{}
	for name := range emitted {
		families[trimTrendStat(name)] = true
	}

	for family := range families {
		assert.Contains(t, text, family, "%s is emitted but absent from METRICS.md", family)
	}

	// The doc names k6 families too, in the migration table; only bench_* names
	// are claims about what this engine emits.
	documented := regexp.MustCompile(`bench_[a-z0-9_]+`).FindAllString(text, -1)
	for _, name := range documented {
		// The doc writes a family with a wildcard, e.g. bench_queue_delay_*.
		name = strings.TrimSuffix(name, "_")
		if _, ok := emitted[name]; ok || families[name] {
			continue
		}
		// A family plus a stat suffix, e.g. bench_http_req_duration_p99.
		assert.True(t, families[trimTrendStat(name)],
			"METRICS.md documents %s, which is not emitted", name)
	}
}

// The label tables are the part a reader copies into a query, so they have to
// match what is on the wire.
func TestMetricsDocNamesEveryLabel(t *testing.T) {
	doc, err := os.ReadFile(filepath.Clean(metricsDocPath))
	require.NoError(t, err)
	text := string(doc)

	labels := map[string]bool{}
	for _, set := range seriesLabels(t) {
		for label := range set {
			labels[label] = true
		}
	}

	for label := range labels {
		assert.Contains(t, text, "`"+label+"`", "label %s is emitted but absent from METRICS.md", label)
	}

	for _, dead := range []string{"expected_response", "error_code", "group", "url"} {
		assert.Contains(t, text, dead,
			"METRICS.md must say what happened to the dropped k6 label %s", dead)
	}
}
