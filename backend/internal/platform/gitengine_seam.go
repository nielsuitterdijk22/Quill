package platform

import (
	"context"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// GitMaintenance assembles the async-maintenance runner (serverless epic,
// B7) over the configured engine: referee + leases are the Postgres-backed
// production adapters, the cache is the same one the /git mount uses. Nil
// semantics never apply — callers only invoke it when Own() mode is on.
func (s *Service) GitMaintenance() *gitengine.Maintenance {
	return &gitengine.Maintenance{
		Engine:  s.gitEngine,
		Cache:   s.gitCache,
		Referee: s.GitReferee(),
		Leases:  s.GitPushLeases(),
	}
}

// ListEngineRepos enumerates provider-independent engine repos (B7 walks
// them for maintenance).
func (s *Service) ListEngineRepos(ctx context.Context) ([]store.GitRepoRef, error) {
	return s.store.ListEngineRepos(ctx)
}

// ResolveRepoForGit resolves project+repo slugs for the git smart-HTTP read
// path (serverless epic, PR B2). Interim resolver: no auth scoping yet (that
// is B6's token exchange); slugs resolve unscoped against the repo table.
// Repos whose content still lives on Forgejo have no manifest in the bucket,
// so downstream hydration naturally 404s until Phase D migrates them.
func (s *Service) ResolveRepoForGit(ctx context.Context, projectSlug, repoSlug string) (store.GitRepoRef, error) {
	return s.store.FindRepoForGit(ctx, projectSlug, repoSlug)
}
