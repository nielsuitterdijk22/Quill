package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// inviteTTL is how long an org invite remains acceptable after it is created.
const inviteTTL = 7 * 24 * time.Hour

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

	// Best-effort: provision the backing Zitadel org so members can be invited by
	// email and, later, sign in against it (org claim -> this tenant). A failure
	// here never fails org creation — the org works Quill-side and invites fall
	// back to shareable links.
	if s.orgProvisionerEnabled() {
		if orgID, perr := s.orgs.CreateOrg(ctx, name); perr != nil {
			s.logger.Warn("could not provision external org", "slug", slug, "error", perr)
		} else if serr := s.store.SetTenantExternalOrg(ctx, db.SetTenantExternalOrgParams{
			ID:            tenant.ID,
			ExternalOrgID: pgtype.Text{String: orgID, Valid: true},
		}); serr != nil {
			s.logger.Error("could not link external org to tenant", "slug", slug, "orgId", orgID, "error", serr)
		} else {
			tenant.ExternalOrgID = pgtype.Text{String: orgID, Valid: true}
		}
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

// ---- members ---------------------------------------------------------------

// OrgMember is a member of an organization with their role.
type OrgMember struct {
	UserID      uuid.UUID
	Username    string
	Email       string
	DisplayName string
	Role        string
	Since       time.Time
}

// ListOrgMembers returns an organization's members. Any member may view the
// roster; a non-member is forbidden.
func (s *Service) ListOrgMembers(ctx context.Context, actor Actor, orgSlug string) ([]OrgMember, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeTenantMember(ctx, actor, tenant.ID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListTenantMembers(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	out := make([]OrgMember, 0, len(rows))
	for _, r := range rows {
		out = append(out, OrgMember{
			UserID:      r.ID,
			Username:    r.Username,
			Email:       r.Email,
			DisplayName: r.DisplayName,
			Role:        r.MemberRole,
			Since:       r.MemberSince,
		})
	}
	return out, nil
}

// UpdateOrgMemberRole changes an existing member's role (admin only). It refuses
// to demote the organization's last admin, which would leave it unmanageable.
func (s *Service) UpdateOrgMemberRole(ctx context.Context, actor Actor, orgSlug string, targetUserID uuid.UUID, role string) error {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	if role != "admin" && role != "member" {
		return fmt.Errorf("%w: role must be 'admin' or 'member'", ErrInvalidInput)
	}
	current, err := s.store.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: targetUserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup member: %w", err)
	}
	if current.Role == "admin" && role == "member" {
		if err := s.ensureNotLastAdmin(ctx, tenant.ID); err != nil {
			return err
		}
	}
	if err := s.store.AddTenantMember(ctx, db.AddTenantMemberParams{
		TenantID: tenant.ID,
		UserID:   targetUserID,
		Role:     role,
	}); err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	return nil
}

// RemoveOrgMember removes a member from the organization (admin only). It refuses
// to remove the last admin.
func (s *Service) RemoveOrgMember(ctx context.Context, actor Actor, orgSlug string, targetUserID uuid.UUID) error {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	current, err := s.store.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: targetUserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup member: %w", err)
	}
	if current.Role == "admin" {
		if err := s.ensureNotLastAdmin(ctx, tenant.ID); err != nil {
			return err
		}
	}
	if err := s.store.RemoveTenantMember(ctx, db.RemoveTenantMemberParams{TenantID: tenant.ID, UserID: targetUserID}); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

// ensureNotLastAdmin returns ErrInvalidInput when the tenant has only one admin,
// so callers can block demoting or removing it.
func (s *Service) ensureNotLastAdmin(ctx context.Context, tenantID uuid.UUID) error {
	admins, err := s.store.CountTenantAdmins(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if admins <= 1 {
		return fmt.Errorf("%w: an organization must keep at least one admin", ErrInvalidInput)
	}
	return nil
}

// ---- invites ---------------------------------------------------------------

// InviteView is a pending invitation in its API-facing form.
type InviteView struct {
	ID        uuid.UUID
	Email     string
	Role      string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// InviteResult is the outcome of creating an invite: the stored invite plus the
// one-time raw token (for building the accept link) and whether the IdP was asked
// to email it.
type InviteResult struct {
	Invite       InviteView
	Token        string
	EmailedByIdP bool
}

// CreateInvite invites a person by email to an organization (admin only). It
// records the invite (revoking any prior pending invite for the same email) and,
// when the org is backed by an external IdP, best-effort asks the IdP to send the
// invitation email. The raw token is returned once so a shareable accept link can
// be built; only its hash is stored.
func (s *Service) CreateInvite(ctx context.Context, actor Actor, orgSlug, email, role string) (InviteResult, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return InviteResult{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return InviteResult{}, err
	}
	email = strings.TrimSpace(email)
	if _, err := mail.ParseAddress(email); err != nil {
		return InviteResult{}, fmt.Errorf("%w: a valid email address is required", ErrInvalidInput)
	}
	if role == "" {
		role = "member"
	}
	if role != "admin" && role != "member" {
		return InviteResult{}, fmt.Errorf("%w: role must be 'admin' or 'member'", ErrInvalidInput)
	}

	raw, err := randomToken(32)
	if err != nil {
		return InviteResult{}, err
	}

	// Replace any existing pending invite for this email so the unique pending
	// index never trips and the newest link is the only valid one.
	if err := s.store.RevokePendingInvitesByEmail(ctx, db.RevokePendingInvitesByEmailParams{
		TenantID: tenant.ID,
		Email:    email,
	}); err != nil {
		return InviteResult{}, fmt.Errorf("revoke prior invites: %w", err)
	}

	invite, err := s.store.CreateInvite(ctx, db.CreateInviteParams{
		TenantID:  tenant.ID,
		Email:     email,
		Role:      role,
		TokenHash: hashToken(raw),
		InvitedBy: uuid.NullUUID{UUID: actor.UserID, Valid: true},
		ExpiresAt: time.Now().Add(inviteTTL),
	})
	if err != nil {
		return InviteResult{}, fmt.Errorf("create invite: %w", err)
	}

	emailed := false
	if s.orgProvisionerEnabled() && tenant.ExternalOrgID.Valid && tenant.ExternalOrgID.String != "" {
		if ierr := s.orgs.InviteUser(ctx, tenant.ExternalOrgID.String, email, ""); ierr != nil {
			s.logger.Warn("could not send invite via external IdP", "org", orgSlug, "email", email, "error", ierr)
		} else {
			emailed = true
		}
	}

	return InviteResult{
		Invite:       inviteView(invite),
		Token:        raw,
		EmailedByIdP: emailed,
	}, nil
}

// ListInvites returns an organization's pending invitations (admin only).
func (s *Service) ListInvites(ctx context.Context, actor Actor, orgSlug string) ([]InviteView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListPendingInvitesByTenant(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	out := make([]InviteView, 0, len(rows))
	for _, r := range rows {
		out = append(out, inviteView(r))
	}
	return out, nil
}

// RevokeInvite cancels a pending invitation (admin only).
func (s *Service) RevokeInvite(ctx context.Context, actor Actor, orgSlug string, inviteID uuid.UUID) error {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	n, err := s.store.RevokeInvite(ctx, db.RevokeInviteParams{ID: inviteID, TenantID: tenant.ID})
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AcceptInvite adds the authenticated actor to the invited organization and marks
// the invite accepted. The raw token from the accept link is the bearer secret:
// possession is acceptance. Returns the org slug so the caller can redirect.
func (s *Service) AcceptInvite(ctx context.Context, actor Actor, rawToken string) (string, error) {
	invite, err := s.store.GetInviteByTokenHash(ctx, hashToken(strings.TrimSpace(rawToken)))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lookup invite: %w", err)
	}
	if invite.Status != "pending" {
		return "", fmt.Errorf("%w: this invitation is no longer valid", ErrInvalidInput)
	}
	if time.Now().After(invite.ExpiresAt) {
		return "", fmt.Errorf("%w: this invitation has expired", ErrInvalidInput)
	}

	tenant, err := s.store.GetTenantByID(ctx, invite.TenantID)
	if err != nil {
		return "", fmt.Errorf("load org: %w", err)
	}

	err = s.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.AddTenantMember(ctx, db.AddTenantMemberParams{
			TenantID: invite.TenantID,
			UserID:   actor.UserID,
			Role:     invite.Role,
		}); err != nil {
			return err
		}
		return q.MarkInviteAccepted(ctx, db.MarkInviteAcceptedParams{
			ID:             invite.ID,
			AcceptedUserID: uuid.NullUUID{UUID: actor.UserID, Valid: true},
		})
	})
	if err != nil {
		return "", fmt.Errorf("accept invite: %w", err)
	}
	return tenant.Slug, nil
}

// inviteView projects a stored invite into its API-facing form (never exposing
// the token hash).
func inviteView(in db.OrgInvite) InviteView {
	return InviteView{
		ID:        in.ID,
		Email:     in.Email,
		Role:      in.Role,
		ExpiresAt: in.ExpiresAt,
		CreatedAt: in.CreatedAt,
	}
}

// hashToken returns the hex-encoded SHA-256 of an invite token; only the hash is
// stored, so a database leak never exposes usable accept links.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
