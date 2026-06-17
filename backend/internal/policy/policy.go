// Package policy is Quill's policy engine: the scope hierarchy, the typed rule
// bodies, and the resolver that merges policies declared at tenant, project, and
// repo scope into a single effective rule for a target.
//
// Policies are stored generically (see the policies table) so new kinds can be
// added without schema changes. This package owns the typed view of those rows
// and the inheritance semantics:
//
//   - Resolution folds the broadest scope inward: tenant -> project -> repo.
//   - A broader scope may set Locked to forbid narrower scopes from weakening it;
//     once any ancestor is locked, narrower scopes may only tighten.
//
// Each kind (branch protection today; environment and artefact promotion later)
// supplies its own rule type and tighten rule but shares the same fold.
package policy

// ScopeType identifies which level of the Tenant -> Project -> Repo hierarchy a
// policy attaches to.
type ScopeType string

const (
	ScopeTenant  ScopeType = "tenant"
	ScopeProject ScopeType = "project"
	ScopeRepo    ScopeType = "repo"
)

// scopeRank orders scopes from broadest (tenant) to narrowest (repo) for the
// resolver fold. Unknown scopes sort last so malformed rows can't mask a repo.
func scopeRank(s ScopeType) int {
	switch s {
	case ScopeTenant:
		return 0
	case ScopeProject:
		return 1
	case ScopeRepo:
		return 2
	default:
		return 3
	}
}

// Kind discriminates the policy domain. The rules JSON is interpreted according
// to the kind.
type Kind string

const (
	KindBranch            Kind = "branch"
	KindEnvironment       Kind = "environment"
	KindArtifactPromotion Kind = "artifact_promotion"
)
