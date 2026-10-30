package comparator

import (
	"fmt"
	"sort"
	"strings"
)

// CorpusExclusions is the method policy --from-jsonl applies at load: the
// methods and namespace prefixes whose calls are dropped before selection.
//
// The default (DefaultCorpusExclusions) is the list the loader has always
// applied: methods unsuitable for cross-client archive comparison, because
// proofs are not always stored and head-dependent answers diverge legitimately
// between nodes at different heads, plus the whole debug_ namespace. A lane
// that runs a parked pair — both nodes fixed at one block, so "head-dependent"
// is no longer a source of difference — can narrow the list with
// --corpus-exclude. Pinnable methods (corpusPinnable: eth_feeHistory) are a
// separate rule and are not part of this policy: they are kept when a block
// override is set and dropped otherwise, whatever the exclusions say.
type CorpusExclusions struct {
	Methods  map[string]struct{}
	Prefixes []string
}

// CorpusExcludeNone is the --corpus-exclude value that disables every
// exclusion. It is a word rather than an empty string so that an empty flag —
// usually a shell expansion that produced nothing — is an error, not a policy.
const CorpusExcludeNone = "none"

// DefaultCorpusExcludeFlag is the --corpus-exclude value equivalent to the
// built-in list, in the syntax ParseCorpusExclusions reads: a trailing "_"
// marks a namespace prefix, anything else is a method name. Prefixes first,
// then methods, each sorted — the order String renders, so the default
// round-trips.
const DefaultCorpusExcludeFlag = "debug_,eth_blockNumber,eth_gasPrice,eth_getProof,eth_maxPriorityFeePerGas,eth_syncing"

// DefaultCorpusExclusions returns the loader's historical policy. It is built
// from DefaultCorpusExcludeFlag so the flag's documented default and the code's
// default cannot drift apart.
func DefaultCorpusExclusions() CorpusExclusions {
	exclusions, err := ParseCorpusExclusions(DefaultCorpusExcludeFlag)
	if err != nil {
		panic(fmt.Sprintf("DefaultCorpusExcludeFlag does not parse: %v", err))
	}
	return exclusions
}

// ParseCorpusExclusions reads a --corpus-exclude value. Entries are separated
// by commas and trimmed; an entry ending in "_" excludes that namespace by
// prefix, any other entry excludes exactly that method. The single word
// CorpusExcludeNone disables exclusions altogether. An empty value, an entry
// that is only "_", an entry containing whitespace, or "none" combined with
// anything else is an error, because each of those is more likely a mistake
// than a policy.
func ParseCorpusExclusions(value string) (CorpusExclusions, error) {
	exclusions := CorpusExclusions{Methods: map[string]struct{}{}}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return exclusions, fmt.Errorf("--corpus-exclude is empty; pass %q to disable exclusions", CorpusExcludeNone)
	}
	entries := strings.Split(trimmed, ",")
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		switch {
		case entry == "":
			return exclusions, fmt.Errorf("--corpus-exclude has an empty entry in %q", value)
		case entry == CorpusExcludeNone:
			if len(entries) != 1 {
				return exclusions, fmt.Errorf("--corpus-exclude %q: %q cannot be combined with other entries", value, CorpusExcludeNone)
			}
			return exclusions, nil
		case strings.ContainsAny(entry, " \t"):
			return exclusions, fmt.Errorf("--corpus-exclude entry %q contains whitespace", entry)
		case entry == "_":
			return exclusions, fmt.Errorf("--corpus-exclude entry %q names no namespace", entry)
		case strings.HasSuffix(entry, "_"):
			exclusions.Prefixes = append(exclusions.Prefixes, entry)
		default:
			exclusions.Methods[entry] = struct{}{}
		}
	}
	sort.Strings(exclusions.Prefixes)
	return exclusions, nil
}

// Excludes answers whether the loader drops method under this policy.
// keepPinnable is true when a block override is set, which is the only thing
// that decides a pinnable method's fate.
func (e CorpusExclusions) Excludes(method string, keepPinnable bool) bool {
	for _, prefix := range e.Prefixes {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	if _, ok := e.Methods[method]; ok {
		return true
	}
	if _, ok := corpusPinnable[method]; ok {
		return !keepPinnable
	}
	return false
}

// SortedMethods returns the excluded method names in a stable order, for the
// load report and the log.
func (e CorpusExclusions) SortedMethods() []string {
	out := make([]string, 0, len(e.Methods))
	for method := range e.Methods {
		out = append(out, method)
	}
	sort.Strings(out)
	return out
}

// String renders the policy in --corpus-exclude syntax, so a log line or a
// report can be pasted back as the flag that reproduces it.
func (e CorpusExclusions) String() string {
	entries := append([]string{}, e.Prefixes...)
	entries = append(entries, e.SortedMethods()...)
	if len(entries) == 0 {
		return CorpusExcludeNone
	}
	return strings.Join(entries, ",")
}

// CorpusPolicy is the load report's record of the exclusion policy that was in
// force, so a consumer reconciling the run reads the effective list from the
// run rather than assuming the default.
type CorpusPolicy struct {
	ExcludedMethods  []string `json:"excluded_methods"`
	ExcludedPrefixes []string `json:"excluded_prefixes"`
	PinnableMethods  []string `json:"pinnable_methods"`
	PinnableKept     bool     `json:"pinnable_kept"`
}

func newCorpusPolicy(exclusions CorpusExclusions, keepPinnable bool) CorpusPolicy {
	pinnable := make([]string, 0, len(corpusPinnable))
	for method := range corpusPinnable {
		pinnable = append(pinnable, method)
	}
	sort.Strings(pinnable)
	prefixes := append([]string{}, exclusions.Prefixes...)
	if prefixes == nil {
		prefixes = []string{}
	}
	return CorpusPolicy{
		ExcludedMethods:  exclusions.SortedMethods(),
		ExcludedPrefixes: prefixes,
		PinnableMethods:  pinnable,
		PinnableKept:     keepPinnable,
	}
}
