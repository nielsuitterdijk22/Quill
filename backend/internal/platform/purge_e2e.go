package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// E2ESlugPrefix is the live-e2e naming convention: every tenant/project/repo a
// live test round mints carries this prefix (the e2e-tester/e2e-tester2 fixture
// users included). It bounds what PurgeE2eDebris considers disposable.
const E2ESlugPrefix = "e2e-"

// PurgeReport summarises a debris purge run for ops visibility.
type PurgeReport struct {
	Tenants  int `json:"tenants"`
	Projects int `json:"projects"`
	Repos    int `json:"repos"`
}

// PurgeE2eDebris removes live-e2e leftovers from a deployment: org tenants and
// their projects/repos with the e2e- slug prefix, plus e2e-prefixed repos
// inside the e2e fixture users' personal projects. It never deletes user rows,
// the default tenant, personal projects themselves, or any tenant/project
// without the prefix — real accounts (even ones that happen to have an
// e2e-named repo in a real project) are left alone. Backs `quill admin
// purge-e2e`. Best-effort per item: failures are logged, not fatal, so a
// partial run can be repeated.
func (s *Service) PurgeE2eDebris(ctx context.Context) (PurgeReport, error) {
	var report PurgeReport

	// 1. Org tenants with the prefix: purge each project (repos in Forgejo and
	// the DB, project policies, the Forgejo org), then drop the tenant.
	tenants, err := s.store.ListOrgTenantsBySlugLike(ctx, E2ESlugPrefix+"%")
	if err != nil {
		return report, fmt.Errorf("list e2e tenants: %w", err)
	}
	purgedTenants := 0
	for _, tenant := range tenants {
		projects, err := s.store.ListProjectsByTenantID(ctx, tenant.ID)
		if err != nil {
			s.logger.Warn("purge-e2e: list projects failed", "tenant", tenant.Slug, "error", err)
			continue
		}
		remaining := 0
		for _, project := range projects {
			repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{
				ProjectID: project.ID, Limit: 1000, Offset: 0,
			})
			if err == nil {
				report.Repos += len(repos)
			}
			if err := s.purgeProject(ctx, project); err != nil {
				remaining++
				s.logger.Warn("purge-e2e: project cleanup failed", "project", project.Slug, "error", err)
				continue
			}
			report.Projects++
		}
		if remaining > 0 {
			continue // leave the tenant so a later run can retry its leftovers
		}
		if err := s.store.DeleteTenant(ctx, tenant.ID); err != nil {
			s.logger.Warn("purge-e2e: tenant delete failed", "tenant", tenant.Slug, "error", err)
			continue
		}
		purgedTenants++
	}
	report.Tenants = purgedTenants

	// 2. E2e fixture users' personal projects: delete only e2e-prefixed repos
	// inside them, keeping the (auto-recreated) project row and the user.
	users, err := s.store.ListUsers(ctx, db.ListUsersParams{Limit: 1000, Offset: 0})
	if err != nil {
		return report, fmt.Errorf("list users: %w", err)
	}
	for _, user := range users {
		if !strings.HasPrefix(user.Username, E2ESlugPrefix) {
			continue
		}
		personal, err := s.store.GetProjectBySlug(ctx, user.Username)
		if err != nil {
			continue // no personal project row
		}
		repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{
			ProjectID: personal.ID, Limit: 1000, Offset: 0,
		})
		if err != nil {
			s.logger.Warn("purge-e2e: list personal repos failed", "user", user.Username, "error", err)
			continue
		}
		for _, repo := range repos {
			if !strings.HasPrefix(repo.Slug, E2ESlugPrefix) {
				continue
			}
			if s.forgejoEnabled() {
				if owner, name, ok := forgejoTarget(repo, personal); ok {
					if derr := s.forgejo.DeleteRepo(ctx, owner, name); derr != nil {
						s.logger.Warn("purge-e2e: forgejo delete repo failed", "repo", repo.Slug, "error", derr)
					}
				}
			}
			s.deleteEngineObjects(ctx, repo, personal)
			if derr := s.store.DeleteRepository(ctx, repo.ID); derr != nil {
				s.logger.Warn("purge-e2e: delete repo row failed", "repo", repo.Slug, "error", derr)
				continue
			}
			if _, derr := s.store.DeletePoliciesByScope(ctx, db.DeletePoliciesByScopeParams{
				ScopeType: string(policy.ScopeRepo),
				ScopeID:   repo.ID,
			}); derr != nil {
				s.logger.Warn("purge-e2e: delete repo policies failed", "repo", repo.ID, "error", derr)
			}
			report.Repos++
		}
	}
	return report, nil
}
