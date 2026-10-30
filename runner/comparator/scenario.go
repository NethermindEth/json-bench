package comparator

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/jsonrpc-bench/runner/types"
)

// A scenario is an ordered list of calls that only mean something together:
// eth_newFilter returns an id, and eth_getFilterLogs is asked about that id.
// Each node runs the scenario's steps in order, on its own, and a later step's
// params may name an earlier step's answer with a $ref token, which is replaced
// by what *that node* answered. Every step is compared like any other call and
// reported as its own result, so a run accounts for each step separately.
//
// A corpus line holds a scenario instead of a call when it carries "scenario"
// and "steps" rather than "method" and "params":
//
//	{"scenario":"log_filter","steps":[
//	  {"method":"eth_newFilter","params":[{"fromBlock":"0x10","toBlock":"0x1f"}]},
//	  {"method":"eth_getFilterLogs","params":[{"$ref":0}]},
//	  {"method":"eth_uninstallFilter","params":[{"$ref":0}]}]}
//
// {"$ref":N} — an object with that single key and an integer N — stands for
// the result of step N, which must come earlier. It may appear anywhere inside
// a step's params. A step whose result a later step references is compared as
// an opaque value: two nodes agree on it when both answered a result of the
// same JSON kind, whatever its value, because a filter id is a handle the node
// chose rather than an answer about the chain.

// ScenarioRefKey is the key of a $ref token.
const ScenarioRefKey = "$ref"

// scenarioIdentityMethod is the method name a whole scenario's identity hashes
// under; see ScenarioID.
const scenarioIdentityMethod = "scenario"

// ScenarioStep is one call of a scenario as the corpus wrote it.
type ScenarioStep struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// ScenarioCall is one loaded scenario on its way to the comparator: its label,
// its identity, and its steps with each step's own identity and call id.
type ScenarioCall struct {
	Label      string
	ScenarioID string
	Steps      []ScenarioStep
	StepIDs    []string
	CallIDs    []string
	// Opaque[i] is true when a later step references step i's result.
	Opaque []bool
}

// ScenarioID is the identity of a whole scenario: RequestID of the method
// "scenario" with the steps, as {method, params} objects, for params. Two
// lines with the same steps are one scenario whatever their labels say, the
// same way two lines with the same call are one request.
func ScenarioID(steps []ScenarioStep) (string, error) {
	return RequestID(scenarioIdentityMethod, scenarioStepsParam(steps))
}

// ScenarioStepID is the identity of step i of the scenario with identity
// scenarioID: RequestID(method, [scenarioID, i, params]). A step's params
// alone do not identify it — eth_uninstallFilter([{"$ref":0}]) is the same in
// every filter scenario — so the scenario and the position are part of it, and
// every step of every scenario in a corpus has an identity of its own.
func ScenarioStepID(scenarioID string, index int, step ScenarioStep) (string, error) {
	params := step.Params
	if params == nil {
		params = []interface{}{}
	}
	return RequestID(step.Method, []interface{}{scenarioID, index, params})
}

func scenarioStepsParam(steps []ScenarioStep) []interface{} {
	out := make([]interface{}, len(steps))
	for i, step := range steps {
		params := step.Params
		if params == nil {
			params = []interface{}{}
		}
		out[i] = map[string]interface{}{"method": step.Method, "params": params}
	}
	return out
}

// scenarioRef reports whether v is a $ref token, and the step it names.
func scenarioRef(v interface{}) (int, bool, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return 0, false, nil
	}
	raw, ok := m[ScenarioRefKey]
	if !ok {
		return 0, false, nil
	}
	if len(m) != 1 {
		return 0, true, fmt.Errorf("a %s token has exactly one key", ScenarioRefKey)
	}
	var index int64
	switch n := raw.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, true, fmt.Errorf("%s %q is not a step index", ScenarioRefKey, n.String())
		}
		index = i
	case float64:
		if n != float64(int64(n)) {
			return 0, true, fmt.Errorf("%s %v is not a step index", ScenarioRefKey, n)
		}
		index = int64(n)
	case int:
		index = int64(n)
	default:
		return 0, true, fmt.Errorf("%s must be an integer step index", ScenarioRefKey)
	}
	if index < 0 || index > 1<<20 {
		return 0, true, fmt.Errorf("%s %d is out of range", ScenarioRefKey, index)
	}
	return int(index), true, nil
}

// scenarioRefs returns every step index v references, walking objects and
// arrays.
func scenarioRefs(v interface{}) ([]int, error) {
	var refs []int
	var walk func(interface{}) error
	walk = func(v interface{}) error {
		index, isRef, err := scenarioRef(v)
		if err != nil {
			return err
		}
		if isRef {
			refs = append(refs, index)
			return nil
		}
		switch t := v.(type) {
		case map[string]interface{}:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if err := walk(t[k]); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, item := range t {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(v)
	return refs, err
}

// validateScenario checks a scenario line: a label, at least one step, every
// step naming a method, and every $ref pointing at an earlier step. It returns
// which steps are referenced. A malformed scenario fails the file it is in,
// like any other line that does not decode (CONTRACT.md §5 of
// rpc-corpus-tools): a scenario that cannot run as written must not shrink
// into a smaller one silently.
func validateScenario(label string, steps []ScenarioStep) ([]bool, error) {
	if label == "" {
		return nil, fmt.Errorf(`a scenario needs a non-empty "scenario" label`)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("scenario %q has no steps", label)
	}
	opaque := make([]bool, len(steps))
	for i, step := range steps {
		if step.Method == "" {
			return nil, fmt.Errorf("scenario %q step %d names no method", label, i)
		}
		refs, err := scenarioRefs(step.Params)
		if err != nil {
			return nil, fmt.Errorf("scenario %q step %d: %w", label, i, err)
		}
		for _, ref := range refs {
			if ref >= i {
				return nil, fmt.Errorf("scenario %q step %d references step %d, which does not come before it", label, i, ref)
			}
			opaque[ref] = true
		}
	}
	return opaque, nil
}

// resolveScenarioParams returns params with every $ref token replaced by the
// result the same node gave for that step. A reference to a step whose answer
// carried no result — an error, or no answer at all — cannot be resolved, and
// the step is not sent to that node.
func resolveScenarioParams(params []interface{}, answers []map[string]interface{}) ([]interface{}, error) {
	var resolve func(interface{}) (interface{}, error)
	resolve = func(v interface{}) (interface{}, error) {
		index, isRef, err := scenarioRef(v)
		if err != nil {
			return nil, err
		}
		if isRef {
			if index >= len(answers) || answers[index] == nil {
				return nil, fmt.Errorf("unresolved %s: step %d has no answer from this node", ScenarioRefKey, index)
			}
			result, ok := answers[index]["result"]
			if !ok {
				return nil, fmt.Errorf("unresolved %s: step %d answered with no result on this node", ScenarioRefKey, index)
			}
			return result, nil
		}
		switch t := v.(type) {
		case map[string]interface{}:
			out := make(map[string]interface{}, len(t))
			for k, item := range t {
				r, err := resolve(item)
				if err != nil {
					return nil, err
				}
				out[k] = r
			}
			return out, nil
		case []interface{}:
			out := make([]interface{}, len(t))
			for i, item := range t {
				r, err := resolve(item)
				if err != nil {
					return nil, err
				}
				out[i] = r
			}
			return out, nil
		}
		return v, nil
	}
	out := make([]interface{}, len(params))
	for i, p := range params {
		r, err := resolve(p)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// opaqueResponse returns a copy of a response whose result is replaced by the
// JSON kind of the result, so two nodes that each answered a handle of their
// own compare equal and a node that answered an error, or null, or a handle of
// a different kind does not.
func opaqueResponse(response map[string]interface{}) map[string]interface{} {
	result, ok := response["result"]
	if !ok || result == nil {
		return response
	}
	out := make(map[string]interface{}, len(response))
	for k, v := range response {
		out[k] = v
	}
	out["result"] = map[string]interface{}{"$opaque": jsonKind(result)}
	return out
}

func jsonKind(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64, int, int64:
		return "number"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}

// scenarioStepCount is the number of steps over scenarios, which is how many
// calls they add to a run.
func scenarioStepCount(scenarios []ScenarioCall) int {
	n := 0
	for _, s := range scenarios {
		n += len(s.Steps)
	}
	return n
}

// runScenario runs one scenario's steps in order. Each step goes to every
// client before the next one starts, so every client sees the steps in the
// scenario's order; each client's params are resolved from its own earlier
// answers. Every step produces its own result, whatever happened to the steps
// before it, so a run always reports as many results as the scenario has steps.
func (c *Comparator) runScenario(scenario ScenarioCall) error {
	answers := make(map[string][]map[string]interface{}, len(c.config.Clients))
	for _, client := range c.config.Clients {
		answers[client.Name] = make([]map[string]interface{}, len(scenario.Steps))
	}
	var mu sync.Mutex

	for i, step := range scenario.Steps {
		index := i
		effective := make(map[string]interface{}, len(c.config.Clients))
		spec := callSpec{
			method:    scenario.CallIDs[i],
			rpcMethod: step.Method,
			params:    step.Params,
			requestID: scenario.StepIDs[i],
			opaque:    scenario.Opaque[i],
			paramsFor: func(client *types.ClientConfig) ([]interface{}, error) {
				mu.Lock()
				prior := answers[client.Name]
				mu.Unlock()
				resolved, err := resolveScenarioParams(step.Params, prior)
				if err != nil {
					return nil, err
				}
				if c.config.BlockOverride != "" {
					resolved = applyBlockOverride(step.Method, resolved, c.config.BlockOverride, c.config.StrictResponseComparison)
				}
				mu.Lock()
				effective[client.Name] = resolved
				mu.Unlock()
				return resolved, nil
			},
			onAnswer: func(client *types.ClientConfig, response map[string]interface{}) {
				mu.Lock()
				answers[client.Name][index] = response
				mu.Unlock()
			},
			metadata: map[string]interface{}{
				"scenario":    scenario.Label,
				"scenario_id": scenario.ScenarioID,
				"step":        index,
				// What each client was actually sent: the $ref tokens resolved
				// against its own answers, then the block override.
				"effective_params": effective,
			},
		}
		if spec.requestID == "" {
			log.Printf("warning: no request identity for %s", spec.method)
		}
		result, err := c.compareCall(spec)
		if err != nil {
			return err
		}
		c.saveResult(result)
	}
	return nil
}
