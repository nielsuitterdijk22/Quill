package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkflowEnvironment returns the deployment environment a workflow targets:
// the `environment:` of its jobs (GitHub syntax, a name or {name, url}).
// A run executes the whole workflow on one CI instance with one secret
// scope, so all jobs that name an environment must name the same one; ""
// means none. Expression-valued names (${{ … }}) are rejected: the gate is
// decided before anything runs.
func WorkflowEnvironment(workflowYAML string) (string, error) {
	var wf struct {
		Jobs map[string]struct {
			Environment yaml.Node `yaml:"environment"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowYAML), &wf); err != nil {
		return "", fmt.Errorf("parse workflow: %w", err)
	}
	names := map[string]bool{}
	for _, j := range wf.Jobs {
		n := j.Environment
		var name string
		switch n.Kind {
		case 0:
			continue
		case yaml.ScalarNode:
			name = n.Value
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "name" {
					name = n.Content[i+1].Value
				}
			}
		default:
			return "", fmt.Errorf("environment must be a name or {name: …}")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.Contains(name, "${{") {
			return "", fmt.Errorf("environment %q: expressions are not supported, name the environment", name)
		}
		names[name] = true
	}
	switch len(names) {
	case 0:
		return "", nil
	case 1:
		for n := range names {
			return n, nil
		}
	}
	all := make([]string, 0, len(names))
	for n := range names {
		all = append(all, n)
	}
	sort.Strings(all)
	return "", fmt.Errorf("jobs target different environments (%s); one workflow deploys to one environment", strings.Join(all, ", "))
}

// HasConcurrency reports whether a workflow declares a top-level
// `concurrency:` group. Quill honours it the way GitHub treats a group's
// waiting runs: only the newest pending push run of the workflow on a ref is
// kept (the group is approximated as workflow + ref; expressions are not
// evaluated).
func HasConcurrency(workflowYAML string) bool {
	var wf struct {
		Concurrency yaml.Node `yaml:"concurrency"`
	}
	if err := yaml.Unmarshal([]byte(workflowYAML), &wf); err != nil {
		return false
	}
	return wf.Concurrency.Kind != 0 && !(wf.Concurrency.Kind == yaml.ScalarNode && wf.Concurrency.Value == "")
}
