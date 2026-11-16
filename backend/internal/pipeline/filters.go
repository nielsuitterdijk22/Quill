package pipeline

import (
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// MatchesPushRef applies a workflow's push filters (GitHub semantics) to the
// pushed ref: `branches` / `branches-ignore` for refs/heads/*, `tags` /
// `tags-ignore` for refs/tags/*. With only branch filters a tag push does not
// trigger, and vice versa; with neither, every push does. `paths` filters
// are not evaluated (every push matches them). Until 2026-11-15 the trigger
// only matched the event name, so a push to any branch ran every push
// workflow — the deploy included.
func MatchesPushRef(workflowYAML, ref string) (bool, error) {
	var wf struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(workflowYAML), &wf); err != nil {
		return false, err
	}
	push, ok := eventNode(&wf.On, "push")
	if !ok {
		return false, nil
	}
	if push == nil || push.Kind != yaml.MappingNode {
		return true, nil // `on: push`, `on: [push]`, `push:` with no filters
	}
	filters := map[string][]string{}
	for i := 0; i+1 < len(push.Content); i += 2 {
		k, v := push.Content[i].Value, push.Content[i+1]
		switch k {
		case "branches", "branches-ignore", "tags", "tags-ignore":
			filters[k] = stringList(v)
		}
	}
	_, hasB := filters["branches"]
	_, hasBI := filters["branches-ignore"]
	_, hasT := filters["tags"]
	_, hasTI := filters["tags-ignore"]
	branchFiltered, tagFiltered := hasB || hasBI, hasT || hasTI

	var name string
	isTag := false
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		name = strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/tags/"):
		name, isTag = strings.TrimPrefix(ref, "refs/tags/"), true
	default:
		name = ref // a bare branch name
	}
	if !branchFiltered && !tagFiltered {
		return true, nil
	}
	if isTag {
		if !tagFiltered {
			return false, nil
		}
		if hasT {
			return filterMatch(filters["tags"], name), nil
		}
		return !filterMatch(filters["tags-ignore"], name), nil
	}
	if !branchFiltered {
		return false, nil
	}
	if hasB {
		return filterMatch(filters["branches"], name), nil
	}
	return !filterMatch(filters["branches-ignore"], name), nil
}

// eventNode finds an event in `on:` (string, list or mapping). The returned
// node is the event's config (nil when it has none).
func eventNode(on *yaml.Node, event string) (*yaml.Node, bool) {
	switch on.Kind {
	case yaml.ScalarNode:
		return nil, on.Value == event
	case yaml.SequenceNode:
		for _, n := range on.Content {
			if n.Value == event {
				return nil, true
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Value == event {
				v := on.Content[i+1]
				if v.Kind == yaml.ScalarNode && (v.Tag == "!!null" || v.Value == "") {
					return nil, true
				}
				return v, true
			}
		}
	}
	return nil, false
}

func stringList(n *yaml.Node) []string {
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	var out []string
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

// filterMatch evaluates a GitHub filter list in order: a pattern includes,
// a "!pattern" excludes again; the last match wins.
func filterMatch(patterns []string, name string) bool {
	matched := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		if globMatch(strings.TrimPrefix(p, "!"), name) {
			matched = !neg
		}
	}
	return matched
}

var globCache sync.Map

// globMatch implements GitHub's filter glob: `*` matches within a path
// segment, `**` across segments, `?` one character, `+` one or more of the
// preceding character, `[...]` a class.
func globMatch(pattern, name string) bool {
	if re, ok := globCache.Load(pattern); ok {
		return re.(*regexp.Regexp).MatchString(name)
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '+':
			b.WriteString("+")
		case '[':
			if j := strings.IndexByte(pattern[i:], ']'); j > 0 {
				b.WriteString(pattern[i : i+j+1])
				i += j
			} else {
				b.WriteString(`\[`)
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return pattern == name
	}
	globCache.Store(pattern, re)
	return re.MatchString(name)
}

// pullRequestDefaultTypes are the activity types a pull_request workflow
// reacts to when it lists none (GitHub semantics).
var pullRequestDefaultTypes = []string{"opened", "synchronize", "reopened"}

// MatchesPullRequest reports whether a workflow's pull_request trigger fires
// for a pull request targeting baseBranch with the given activity (action:
// "opened", "synchronize", "reopened"). It applies `types` (default: opened,
// synchronize, reopened) and the `branches` / `branches-ignore` filters,
// which — as on GitHub — match the pull request's BASE branch. `paths`
// filters are not evaluated.
func MatchesPullRequest(workflowYAML, baseBranch, action string) (bool, error) {
	var wf struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(workflowYAML), &wf); err != nil {
		return false, err
	}
	pr, ok := eventNode(&wf.On, "pull_request")
	if !ok {
		return false, nil
	}
	types := pullRequestDefaultTypes
	filters := map[string][]string{}
	if pr != nil && pr.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(pr.Content); i += 2 {
			k, v := pr.Content[i].Value, pr.Content[i+1]
			switch k {
			case "types":
				types = stringList(v)
			case "branches", "branches-ignore":
				filters[k] = stringList(v)
			}
		}
	}
	allowed := false
	for _, t := range types {
		if t == action {
			allowed = true
		}
	}
	if !allowed {
		return false, nil
	}
	base := strings.TrimPrefix(baseBranch, "refs/heads/")
	if _, has := filters["branches"]; has {
		return filterMatch(filters["branches"], base), nil
	}
	if _, has := filters["branches-ignore"]; has {
		return !filterMatch(filters["branches-ignore"], base), nil
	}
	return true, nil
}
