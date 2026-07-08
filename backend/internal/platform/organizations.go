package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Organizations are org-kind tenants: a shared workspace with its own admins and
// members, distinct from the per-account personal tenant every user gets at
// registration. An org owns projects like any tenant, but adds a management
// surface — members, SSO, and org-wide (tenant-scoped) policies — gated by
// tenant_members roles (see authorizeTenantAdmin). Creating an org provisions the
// tenant, makes the creator its admin, and seeds a same-named first project
// (mapped 1:1 to a Forgejo org) so the org is immediately usable.

// OrganizationSummary is an org tenant paired with the caller's role in it, for
// nav and listing.
type OrganizationSummary struct {
	Slug string
	Name string
	Role string
}

// CreateOrganization provisions a new organization for the actor: a dedicated
// org-kind tenant with the creator as admin, plus a same-named project under it.
// The project reuses CreateProject so Forgejo org provisioning and project
// ownership stay identical to a normal project. On project failure the org tenant
// is rolled back so no orphan org remains.
func (s *Service) CreateOrganization(ctx context.Context, actor Actor, slug, name string) (db.Tenant, db.Project, error) {
	slug = normalizeSlug(slug)
	name = strings.TrimSpace(name)
	if !validSlug(slug) {
		return db.Tenant{}, db.Project{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
	}
	if name == "" {
		name = slug
	}

	// Fail fast on a taken tenant slug for a friendlier error than the unique
	// violation. (Project-slug collisions are caught by CreateProject below.)
	if _, err := s.store.GetTenantBySlug(ctx, slug); err == nil {
		return db.Tenant{}, db.Project{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Tenant{}, db.Project{}, fmt.Errorf("lookup tenant: %w", err)
	}

	// Create the org tenant and make the creator its admin in one transaction.
	var tenant db.Tenant
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		t, err := q.CreateOrgTenant(ctx, db.CreateOrgTenantParams{Slug: slug, Name: name})
		if err != nil {
			return err
		}
		if err := q.AddTenantMember(ctx, db.AddTenantMemberParams{
			TenantID: t.ID,
			UserID:   actor.UserID,
			Role:     "admin",
		}); err != nil {
			return err
		}
		tenant = t
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.Tenant{}, db.Project{}, ErrConflict
		}
		return db.Tenant{}, db.Project{}, fmt.Errorf("create organization: %w", err)
	}

	// Seed the org's first project under the new tenant by pointing the actor's
	// tenant at it. CreateProject provisions the Forgejo org and records ownership.
	orgActor := actor
	orgActor.TenantID = tenant.ID
	project, err := s.CreateProject(ctx, orgActor, CreateProjectInput{Slug: slug, Name: name})
	if err != nil {
		// Compensate: drop the org tenant (cascades tenant_members) so a failed
		// first project doesn't strand an empty org. Use a detached context so
		// cleanup still runs on a cancelled request.
		cctx, cancel := detachedContext(ctx)
		defer cancel()
		if delErr := s.store.DeleteTenant(cctx, tenant.ID); delErr != nil {
			s.logger.Error("failed to roll back org tenant", "tenant", tenant.ID, "error", delErr)
		}
		return db.Tenant{}, db.Project{}, err
	}
	return tenant, project, nil
}

// ListOrganizations returns the org tenants the actor is a member of, with their
// role, for nav and the org switcher.
func (s *Service) ListOrganizations(ctx context.Context, actor Actor) ([]OrganizationSummary, error) {
	rows, err := s.store.ListOrgTenantsForUser(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	out := make([]OrganizationSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, OrganizationSummary{Slug: r.Slug, Name: r.Name, Role: r.MemberRole})
	}
	return out, nil
}
