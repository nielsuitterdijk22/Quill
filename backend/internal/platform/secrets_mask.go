package platform

import (
	"sort"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

// secretMask redacts known secret values from log output. It is built from a
// run's resolved secrets and applied to every streamed log line and to the
// stored step logs, so a workflow that echoes a secret cannot leak it through
// Quill's UI or database.
//
// Masking is a plain substring replacement. Like GitHub Actions it works
// line-by-line, so a multi-line secret is also registered by its individual
// lines. Very short values (< minMaskLen) are skipped to avoid shredding
// unrelated log text; such secrets are discouraged anyway.
type secretMask struct {
	// values are the substrings to redact, longest first so an overlapping
	// shorter value never pre-empts a longer one.
	values []string
}

// minMaskLen is the shortest secret value (or line thereof) that is masked.
const minMaskLen = 3

// maskReplacement is what a matched secret value is replaced with.
const maskReplacement = "***"

// newSecretMasker builds a masker from name→value secrets. A nil/empty map
// yields a masker that leaves input untouched.
func newSecretMasker(secrets map[string]string) *secretMask {
	seen := make(map[string]struct{})
	var values []string
	add := func(v string) {
		if len(v) < minMaskLen {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		values = append(values, v)
	}
	for _, v := range secrets {
		add(v)
		// Register individual lines too, so a multi-line secret is still masked
		// when the runner emits it one line at a time.
		if strings.ContainsAny(v, "\r\n") {
			for _, line := range strings.FieldsFunc(v, func(r rune) bool { return r == '\r' || r == '\n' }) {
				add(line)
			}
		}
	}
	// Longest first: mask the most specific value before any substring of it.
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return &secretMask{values: values}
}

// mask redacts every registered secret value found in s.
func (m *secretMask) mask(s string) string {
	if m == nil || len(m.values) == 0 || s == "" {
		return s
	}
	for _, v := range m.values {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, maskReplacement)
		}
	}
	return s
}

// maskResult redacts secrets from every step's stored logs in place.
func (m *secretMask) maskResult(result *pipeline.RunResult) {
	if m == nil || len(m.values) == 0 || result == nil {
		return
	}
	for ji := range result.Jobs {
		for si := range result.Jobs[ji].Steps {
			result.Jobs[ji].Steps[si].Logs = m.mask(result.Jobs[ji].Steps[si].Logs)
		}
	}
}
