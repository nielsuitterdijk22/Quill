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
	}

	var created db.Project
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		project, err := q.CreateProject(ctx, db.CreateProjectParams{
			TenantID:    tenantID,
			Slug:        slug,
			Name:        name,
			Description: strings.TrimSpace(in.Description),
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
