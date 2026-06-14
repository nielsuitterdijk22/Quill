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

// CreateOrgInput is the payload for creating an organization.
type CreateOrgInput struct {
	Slug        string
	Name        string
	Description string
}

// CreateOrg provisions an organization. With Forgejo enabled it creates the
// Forgejo org first, then records the org, its default owning team, and the
// creator's memberships in one Postgres transaction; if that transaction fails
// the Forgejo org is deleted so the two systems don't drift.
func (s *Service) CreateOrg(ctx context.Context, creatorID uuid.UUID, in CreateOrgInput) (db.Organization, error) {
	slug := normalizeSlug(in.Slug)
	name := strings.TrimSpace(in.Name)
	if !slugRe.MatchString(slug) {
		return db.Organization{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.' and start alphanumeric", ErrInvalidInput)
	}
	if name == "" {
		name = slug
	}

	// Fail fast on a known-taken slug to avoid creating an orphan Forgejo org.
	if _, err := s.store.GetOrganizationBySlug(ctx, slug); err == nil {
		return db.Organization{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Organization{}, fmt.Errorf("lookup org: %w", err)
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
			return db.Organization{}, fmt.Errorf("forgejo create org: %w", err)
		}
		forgejoName = pgtype.Text{String: org.Handle(), Valid: true}
	}

	var created db.Organization
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		org, err := q.CreateOrganization(ctx, db.CreateOrganizationParams{
			Slug:        slug,
			Name:        name,
			Description: strings.TrimSpace(in.Description),
		})
		if err != nil {
			return err
		}
		if forgejoName.Valid {
			org, err = q.SetOrganizationForgejoName(ctx, db.SetOrganizationForgejoNameParams{
				ID:             org.ID,
				ForgejoOrgName: forgejoName,
			})
			if err != nil {
				return err
			}
		}
		// Every org gets a required owning team; the creator owns the org and
		// maintains that team so repos have a valid owning team immediately.
		team, err := q.CreateTeam(ctx, db.CreateTeamParams{
			OrgID:       org.ID,
			Slug:        defaultOwningTeamSlug,
			Name:        "Owners",
			Description: "Default owning team for " + org.Slug,
		})
		if err != nil {
			return err
		}
		if err := q.AddOrgMember(ctx, db.AddOrgMemberParams{
			OrgID:  org.ID,
			UserID: creatorID,
			Role:   "owner",
		}); err != nil {
			return err
		}
		if err := q.AddTeamMember(ctx, db.AddTeamMemberParams{
			TeamID: team.ID,
			UserID: creatorID,
			Role:   "maintainer",
		}); err != nil {
			return err
		}
		created = org
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
			return db.Organization{}, ErrConflict
		}
		return db.Organization{}, err
	}
	return created, nil
}

// ListOrgs returns organizations ordered by slug.
func (s *Service) ListOrgs(ctx context.Context, limit, offset int32) ([]db.Organization, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListOrganizations(ctx, db.ListOrganizationsParams{Limit: limit, Offset: offset})
}

// GetOrg returns an organization by slug for an authorized actor, or ErrNotFound
// when it doesn't exist / ErrForbidden when the actor isn't a member.
func (s *Service) GetOrg(ctx context.Context, actor Actor, slug string) (db.Organization, error) {
	org, err := s.getOrg(ctx, slug)
	if err != nil {
		return db.Organization{}, err
	}
	if err := s.authorizeOrgMember(ctx, actor, org.ID); err != nil {
		return db.Organization{}, err
	}
	return org, nil
}

// getOrg loads an organization by slug without an authorization check, for
// internal callers that authorize separately.
func (s *Service) getOrg(ctx context.Context, slug string) (db.Organization, error) {
	org, err := s.store.GetOrganizationBySlug(ctx, normalizeSlug(slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Organization{}, ErrNotFound
	}
	return org, err
}
