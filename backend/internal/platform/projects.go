package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/projectsync"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// CreateProjectInput is the payload for creating a project.
type CreateProjectInput struct {
	Slug        string
	Name        string
	Description string
}

// CreateProject provisions a project under the actor's tenant. With Forgejo
// enabled it creates the Forgejo org first, then records the project and the
// creator's membership in one Postgres transaction; if that transaction fails
// the Forgejo org is deleted so the two systems don't drift.
func (s *Service) CreateProject(ctx context.Context, actor Actor, in CreateProjectInput) (db.Project, error) {
	slug := normalizeSlug(in.Slug)
	name := strings.TrimSpace(in.Name)
	if !validSlug(slug) {
		return db.Project{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
	}
	if name == "" {
		name = slug
	}

	// Use the actor's tenant when set (Clerk multi-tenant), else fall back to the
	// seeded default tenant for single-tenant / local-auth deployments.
	var tenantID uuid.UUID
	if actor.TenantID != (uuid.UUID{}) {
		tenantID = actor.TenantID
	} else {
		tenant, err := s.defaultTenant(ctx)
		if err != nil {
			return db.Project{}, err
		}
		tenantID = tenant.ID
	}

	// Fail fast on a known-taken slug to avoid creating an orphan Forgejo org.
	if _, err := s.store.GetProjectBySlug(ctx, slug); err == nil {
		return db.Project{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Project{}, fmt.Errorf("lookup project: %w", err)
	}

	var forgejoName pgtype.Text
	if s.forgejoEnabled() {
		org, err := s.forgejo.CreateOrg(ctx, forgejo.CreateOrgOptions{
			Name:        slug,
			FullName:    name,
			Description: strings.TrimSpace(in.Description),
			Visibility:  "private",
		})
		if err != nil {
			return db.Project{}, fmt.Errorf("forgejo create org: %w", err)
		}
		forgejoName = pgtype.Text{String: org.Handle(), Valid: true}
		// Add the creating user to the org so they can clone/push repos in it.
		// This is best-effort: failure doesn't roll back the org or project.
		if actorUser, uerr := s.store.GetUserByID(ctx, actor.UserID); uerr == nil &&
			actorUser.ForgejoUsername.Valid && actorUser.ForgejoUsername.String != "" {
			if merr := s.forgejo.AddOrgMember(ctx, org.Handle(), actorUser.ForgejoUsername.String); merr != nil {
				s.logger.Warn("failed to add creator to forgejo org", "org", org.Handle(), "user", actorUser.ForgejoUsername.String, "error", merr)
			}
		}
	}

	var created db.Project
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		project, err := q.CreateProject(ctx, db.CreateProjectParams{
			TenantID:    tenantID,
			Slug:        slug,
			Name:        name,
			Description: strings.TrimSpace(in.Description),
			IsPersonal:  false,
		})
		if err != nil {
			return err
		}
		if forgejoName.Valid {
			project, err = q.SetProjectForgejoName(ctx, db.SetProjectForgejoNameParams{
				ID:             project.ID,
				ForgejoOrgName: forgejoName,
			})
			if err != nil {
				return err
			}
		}
		// The creator owns the project so they can manage repos and membership
		// immediately.
		if err := q.AddProjectMember(ctx, db.AddProjectMemberParams{
			ProjectID: project.ID,
			UserID:    actor.UserID,
			Role:      "owner",
		}); err != nil {
			return err
		}
		// Mirror the new project to Tempo. Enqueued in this same transaction, so
		// the event exists if and only if the project commit succeeds.
		if err := s.enqueueProjectEvent(ctx, q, projectsync.EventCreate, project); err != nil {
			return err
		}
		created = project
		return nil
	})
	if err != nil {
		// Compensate: drop the Forgejo org we created so it isn't orphaned. Use a
		// detached context so cleanup still runs when the failure was a cancelled
		// or timed-out request context.
		if forgejoName.Valid {
			cctx, cancel := detachedContext(ctx)
			defer cancel()
			if delErr := s.forgejo.DeleteOrg(cctx, forgejoName.String); delErr != nil {
				s.logger.Error("failed to roll back forgejo org", "org", forgejoName.String, "error", delErr)
			}
		}
		if isUniqueViolation(err) {
			return db.Project{}, ErrConflict
		}
		return db.Project{}, err
	}
	return created, nil
}

// CreatePersonalProject provisions a personal-namespace project for userID.
// The project slug equals the username so /{username} routes resolve to it.
// Idempotent: returns nil without error if the slug is already taken (a previous
// successful call or a race). Called from Clerk post-provisioning; must never
// fail the login response.
func (s *Service) CreatePersonalProject(ctx context.Context, userID uuid.UUID, username string) error {
	slug := normalizeSlug(username)
	tenant, err := s.defaultTenant(ctx)
	if err != nil {
		return err
	}
	// Idempotency: a personal project's slug equals the (globally unique) username,
	// so an existing project with this slug is unambiguously this user's namespace.
	// Ensure the caller is a member rather than returning early — otherwise a project
	// that exists without the membership row (e.g. a partially-completed earlier run)
	// would leave the user unable to see or import into their own workspace, surfacing
	// later as a 404 on import.
	if existing, err := s.store.GetProjectBySlug(ctx, slug); err == nil {
		if aerr := s.store.AddProjectMember(ctx, db.AddProjectMemberParams{
			ProjectID: existing.ID,
			UserID:    userID,
			Role:      "owner",
		}); aerr != nil {
			return fmt.Errorf("ensure personal project membership: %w", aerr)
		}
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lookup personal project: %w", err)
	}
	return s.store.InTx(ctx, func(q *db.Queries) error {
		project, err := q.CreateProject(ctx, db.CreateProjectParams{
			TenantID:    tenant.ID,
			Slug:        slug,
			Name:        username,
			Description: "",
			IsPersonal:  true,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return nil // race: another goroutine already created it
			}
			return err
		}
		if err := q.AddProjectMember(ctx, db.AddProjectMemberParams{
			ProjectID: project.ID,
			UserID:    userID,
			Role:      "owner",
		}); err != nil {
			return err
		}
		// Personal projects are projects too; mirror them like any other so the
		// backfill and the live path agree on what Tempo sees.
		return s.enqueueProjectEvent(ctx, q, projectsync.EventCreate, project)
	})
}

// PurgeOwnedProjects deletes every project the user solely owns — its repos
// (in Forgejo and the DB), policies, Forgejo org and the project row — so that
// account deletion is a true erasure and a later re-signup can re-import the
// same repos without "already exists" conflicts. Shared projects (a non-personal
// project with other members) are left intact; only the caller's membership is
// removed later when the user row is deleted. Best-effort and idempotent: a
// missing Forgejo resource is treated as already gone, and per-project failures
// are logged and skipped rather than aborting the whole purge.
func (s *Service) PurgeOwnedProjects(ctx context.Context, userID uuid.UUID) error {
	projects, err := s.store.ListProjectsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list user projects: %w", err)
	}
	for _, row := range projects {
		members, err := s.store.ListProjectMembers(ctx, row.ID)
		if err != nil {
			s.logger.Warn("purge: list members failed; skipping project", "project", row.Slug, "error", err)
			continue
		}
		// Leave shared org projects in place — deleting them would destroy other
		// members' work. Personal projects (and solo-owned ones) are fully removed.
		if !row.IsPersonal && len(members) > 1 {
			continue
		}

		project := db.Project{
			ID:             row.ID,
			TenantID:       row.TenantID,
			Slug:           row.Slug,
			Name:           row.Name,
			Description:    row.Description,
			ForgejoOrgName: row.ForgejoOrgName,
			IsPersonal:     row.IsPersonal,
		}
		if err := s.purgeProject(ctx, project); err != nil {
			s.logger.Warn("purge: project cleanup failed", "project", row.Slug, "error", err)
		}
	}
	return nil
}

// purgeProject removes one project's repositories (Forgejo + DB), its policies,
// its Forgejo org, and finally the project row. repositories.project_id is
// ON DELETE RESTRICT, so repos must go before the project row.
func (s *Service) purgeProject(ctx context.Context, project db.Project) error {
	repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{
		ProjectID: project.ID,
		Limit:     1000,
		Offset:    0,
	})
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}
	for _, repo := range repos {
		if s.forgejoEnabled() {
			if owner, name, ok := forgejoTarget(repo, project); ok {
				if derr := s.forgejo.DeleteRepo(ctx, owner, name); derr != nil && !forgejo.NotFound(derr) {
					s.logger.Warn("purge: forgejo delete repo failed", "owner", owner, "name", name, "error", derr)
				}
			}
		}
		if derr := s.store.DeleteRepository(ctx, repo.ID); derr != nil {
			return fmt.Errorf("delete repo %s: %w", repo.Slug, derr)
		}
		if _, derr := s.store.DeletePoliciesByScope(ctx, db.DeletePoliciesByScopeParams{
			ScopeType: string(policy.ScopeRepo),
			ScopeID:   repo.ID,
		}); derr != nil {
			s.logger.Warn("purge: delete repo policies failed", "repo", repo.ID, "error", derr)
		}
	}

	if _, derr := s.store.DeletePoliciesByScope(ctx, db.DeletePoliciesByScopeParams{
		ScopeType: string(policy.ScopeProject),
		ScopeID:   project.ID,
	}); derr != nil {
		s.logger.Warn("purge: delete project policies failed", "project", project.ID, "error", derr)
	}

	// Personal projects live under the user's Forgejo namespace (removed when the
	// Forgejo user is deleted), so only org-backed projects own a Forgejo org.
	if s.forgejoEnabled() && project.ForgejoOrgName.Valid && project.ForgejoOrgName.String != "" {
		if derr := s.forgejo.DeleteOrg(ctx, project.ForgejoOrgName.String); derr != nil && !forgejo.NotFound(derr) {
			s.logger.Warn("purge: forgejo delete org failed", "org", project.ForgejoOrgName.String, "error", derr)
		}
	}

	// Delete the project row and enqueue the mirror delete event in one
	// transaction: Tempo archives (never cascade-deletes) the mirrored project in
	// response. The outbox row deliberately has no FK to projects, so it outlives
	// the row it describes.
	if derr := s.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.DeleteProject(ctx, project.ID); err != nil {
			return err
		}
		return s.enqueueProjectEvent(ctx, q, projectsync.EventDelete, project)
	}); derr != nil {
		return fmt.Errorf("delete project %s: %w", project.Slug, derr)
	}
	return nil
}

// enqueueProjectEvent writes a project-mirror outbox row for project using the
// transaction-scoped queries q, so the event is emitted atomically with the
// project mutation. It resolves the tenant slug because the mirror event keys
// the Tempo org by tenant slug, not by id.
func (s *Service) enqueueProjectEvent(ctx context.Context, q *db.Queries, typ projectsync.EventType, project db.Project) error {
	tenant, err := q.GetTenantByID(ctx, project.TenantID)
	if err != nil {
		return fmt.Errorf("resolve tenant for project sync: %w", err)
	}
	ev := projectsync.NewEvent(typ, project.ID, project.Slug, project.Name, tenant.Slug)
	return projectsync.Enqueue(ctx, q, ev)
}

// ListProjects returns projects ordered by slug.
func (s *Service) ListProjects(ctx context.Context, limit, offset int32) ([]db.Project, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListProjects(ctx, db.ListProjectsParams{Limit: limit, Offset: offset})
}

// ListMyProjects returns the projects the actor belongs to, ordered by slug.
func (s *Service) ListMyProjects(ctx context.Context, actor Actor) ([]db.ListProjectsByUserRow, error) {
	projects, err := s.store.ListProjectsByUser(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list user projects: %w", err)
	}
	return projects, nil
}

// GetProject returns a project by slug for an authorized actor, or ErrNotFound
// when it doesn't exist / ErrForbidden when the actor isn't a member.
func (s *Service) GetProject(ctx context.Context, actor Actor, slug string) (db.Project, error) {
	project, err := s.getProject(ctx, slug)
	if err != nil {
		return db.Project{}, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Project{}, err
	}
	return project, nil
}

// getProject loads a project by slug without an authorization check, for
// internal callers that authorize separately.
func (s *Service) getProject(ctx context.Context, slug string) (db.Project, error) {
	project, err := s.store.GetProjectBySlug(ctx, normalizeSlug(slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Project{}, ErrNotFound
	}
	return project, err
}

// defaultTenant resolves the seeded default tenant, creating it if a fresh
// database somehow lacks the seed row. Projects attach to it until multi-tenant
// management lands.
func (s *Service) defaultTenant(ctx context.Context) (db.Tenant, error) {
	tenant, err := s.store.GetTenantBySlug(ctx, defaultTenantSlug)
	if err == nil {
		return tenant, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Tenant{}, fmt.Errorf("lookup default tenant: %w", err)
	}
	tenant, err = s.store.CreateTenant(ctx, db.CreateTenantParams{Slug: defaultTenantSlug, Name: "Default"})
	if err != nil {
		// A racing creator may have inserted it first; re-read before giving up.
		if isUniqueViolation(err) {
			if t, gerr := s.store.GetTenantBySlug(ctx, defaultTenantSlug); gerr == nil {
				return t, nil
			}
		}
		return db.Tenant{}, fmt.Errorf("create default tenant: %w", err)
	}
	return tenant, nil
}
