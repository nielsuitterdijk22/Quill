package platform

import (
	"context"

	"github.com/nielsuitterdijk22/quill/internal/store"
)

// ResolveRepoForGit resolves project+repo slugs for the git smart-HTTP read
// path (serverless epic, PR B2). Interim resolver: no auth scoping yet (that
// is B6's token exchange); slugs resolve unscoped against the repo table.
// Repos whose content still lives on Forgejo have no manifest in the bucket,
// so downstream hydration naturally 404s until Phase D migrates them.
func (s *Service) ResolveRepoForGit(ctx context.Context, projectSlug, repoSlug string) (store.GitRepoRef, error) {
	return s.store.FindRepoForGit(ctx, projectSlug, repoSlug)
}
