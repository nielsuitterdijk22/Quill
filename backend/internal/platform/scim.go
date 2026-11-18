package platform

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// SCIM 2.0 provisioning (RFC 7643/7644) for organizations. An org admin mints a
// provisioning token; the org's identity provider then creates, updates and
// deprovisions members and groups through /scim/v2. The HTTP/JSON protocol
// lives in the server package; this file holds the rules:
//
//   - A token addresses exactly one tenant; every lookup is tenant-scoped and
//     SCIM ids are per-tenant record ids, never global user ids.
//   - Provisioning matches an existing Quill account by email, else creates an
//     account that links to the person's first sign-in (auth.linkProvisioned).
//     SCIM never rewrites a person's global profile or email.
//   - Deprovisioning (active=false or DELETE) always works — even for a locked
//     org, even for the last admin — and removes the org membership and every
//     group membership in the org at once. Creating or reactivating members
//     requires an active (paid or trialing) org, like invites.
//   - Seats follow membership (syncTeamSeats).

// SCIMTokenPrefix marks Quill SCIM bearer tokens.
const SCIMTokenPrefix = "qsc_"

// SCIMUserInput is the desired state of a provisioned person.
type SCIMUserInput struct {
	UserName    string
	ExternalID  string
	Email       string
	GivenName   string
	FamilyName  string
	DisplayName string
	Active      bool
}

// scimAllowedPlan reports the plans that may use SCIM (locked ones can still
// read/configure; writes are refused by enforceTenantActive).
func scimAllowedPlan(plan string) bool {
	switch plan {
	case PlanTeam, PlanInactive, PlanLapsed, PlanEnterprise, PlanInternal, PlanBeta:
		return true
	}
	return false
}

// ---- tokens (org admin, via the regular API) --------------------------------

// SCIMTokenView is a provisioning token's metadata.
type SCIMTokenView = store.SCIMToken

// CreateSCIMToken mints a provisioning token for an organization (tenant admin
// only, Team organizations). The raw token is returned once and only its hash
// is stored.
func (s *Service) CreateSCIMToken(ctx context.Context, actor Actor, orgSlug, name string) (string, SCIMTokenView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return "", SCIMTokenView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return "", SCIMTokenView{}, err
	}
	if !scimAllowedPlan(tenant.Plan) {
		return "", SCIMTokenView{}, fmt.Errorf("%w: SCIM provisioning is part of the Team plan", ErrLimitReached)
	}
	if err := s.enforceTenantActive(ctx, tenant); err != nil {
		return "", SCIMTokenView{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "SCIM"
	}
	if len(name) > 100 {
		return "", SCIMTokenView{}, fmt.Errorf("%w: name is too long", ErrInvalidInput)
	}
	raw, err := randomToken(32)
	if err != nil {
		return "", SCIMTokenView{}, err
	}
	raw = SCIMTokenPrefix + raw
	tok, err := s.store.CreateSCIMToken(ctx, tenant.ID, name, hashToken(raw), uuid.NullUUID{UUID: actor.UserID, Valid: actor.UserID != uuid.Nil})
	if err != nil {
		return "", SCIMTokenView{}, fmt.Errorf("create scim token: %w", err)
	}
	return raw, tok, nil
}

// ListSCIMTokens lists an organization's provisioning tokens (tenant admin).
func (s *Service) ListSCIMTokens(ctx context.Context, actor Actor, orgSlug string) ([]SCIMTokenView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return nil, err
	}
	return s.store.ListSCIMTokens(ctx, tenant.ID)
}

// RevokeSCIMToken deletes one of an organization's provisioning tokens.
func (s *Service) RevokeSCIMToken(ctx context.Context, actor Actor, orgSlug string, id uuid.UUID) error {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	ok, err := s.store.DeleteSCIMToken(ctx, tenant.ID, id)
	if err != nil {
		return fmt.Errorf("revoke scim token: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// SCIMAuthenticate resolves a bearer token to the tenant it provisions.
func (s *Service) SCIMAuthenticate(ctx context.Context, raw string) (db.Tenant, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, SCIMTokenPrefix) {
		return db.Tenant{}, ErrForbidden
	}
	tenantID, ok, err := s.store.SCIMTenantByTokenHash(ctx, hashToken(raw))
	if err != nil {
		return db.Tenant{}, fmt.Errorf("lookup scim token: %w", err)
	}
	if !ok {
		return db.Tenant{}, ErrForbidden
	}
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return db.Tenant{}, fmt.Errorf("lookup tenant: %w", err)
	}
	if !scimAllowedPlan(tenant.Plan) {
		return db.Tenant{}, ErrForbidden
	}
	return tenant, nil
}

// ---- users -------------------------------------------------------------------

// scimEmail picks the address to match/create an account with.
func scimEmail(in SCIMUserInput) (string, error) {
	email := strings.TrimSpace(in.Email)
	if email == "" && strings.Contains(in.UserName, "@") {
		email = strings.TrimSpace(in.UserName)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", fmt.Errorf("%w: a valid email (emails[primary] or an email-shaped userName) is required", ErrInvalidInput)
	}
	return strings.ToLower(email), nil
}

func validSCIMUser(in SCIMUserInput) error {
	if strings.TrimSpace(in.UserName) == "" {
		return fmt.Errorf("%w: userName is required", ErrInvalidInput)
	}
	for _, f := range []string{in.UserName, in.ExternalID, in.Email, in.GivenName, in.FamilyName, in.DisplayName} {
		if len(f) > 320 {
			return fmt.Errorf("%w: attribute too long", ErrInvalidInput)
		}
	}
	return nil
}

// SCIMCreateUser provisions a person into the tenant.
func (s *Service) SCIMCreateUser(ctx context.Context, tenant db.Tenant, in SCIMUserInput) (store.SCIMUser, error) {
	if err := validSCIMUser(in); err != nil {
		return store.SCIMUser{}, err
	}
	email, err := scimEmail(in)
	if err != nil {
		return store.SCIMUser{}, err
	}
	if in.Active {
		if err := s.enforceTenantActive(ctx, tenant); err != nil {
			return store.SCIMUser{}, err
		}
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		user, err = s.createProvisionedUser(ctx, in, email)
	}
	if err != nil {
		return store.SCIMUser{}, fmt.Errorf("resolve account: %w", err)
	}
	rec, err := s.store.InsertSCIMUser(ctx, store.SCIMUser{
		TenantID: tenant.ID, UserID: user.ID, UserName: strings.TrimSpace(in.UserName), ExternalID: in.ExternalID,
		Email: email, GivenName: in.GivenName, FamilyName: in.FamilyName, DisplayName: in.DisplayName, Active: in.Active,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.SCIMUser{}, ErrConflict
		}
		return store.SCIMUser{}, fmt.Errorf("record scim user: %w", err)
	}
	if in.Active {
		if err := s.scimActivate(ctx, tenant.ID, user.ID); err != nil {
			return store.SCIMUser{}, err
		}
	}
	return rec, nil
}

// SCIMGetUser loads a provisioned person.
func (s *Service) SCIMGetUser(ctx context.Context, tenant db.Tenant, id uuid.UUID) (store.SCIMUser, error) {
	u, err := s.store.GetSCIMUser(ctx, tenant.ID, id)
	if errors.Is(err, store.ErrSCIMNotFound) {
		return store.SCIMUser{}, ErrNotFound
	}
	return u, err
}

// SCIMListUsers pages provisioned people (offset 0-based).
func (s *Service) SCIMListUsers(ctx context.Context, tenant db.Tenant, f store.SCIMUserFilter, offset, limit int) ([]store.SCIMUser, int, error) {
	return s.store.ListSCIMUsers(ctx, tenant.ID, f, offset, limit)
}

// SCIMReplaceUser applies a person's full desired state (PUT, and PATCH after
// the server merged the operations). Deactivation deprovisions the org access
// at once; reactivation restores plain membership (groups are re-pushed by
// the IdP).
func (s *Service) SCIMReplaceUser(ctx context.Context, tenant db.Tenant, id uuid.UUID, in SCIMUserInput) (store.SCIMUser, error) {
	cur, err := s.SCIMGetUser(ctx, tenant, id)
	if err != nil {
		return store.SCIMUser{}, err
	}
	if err := validSCIMUser(in); err != nil {
		return store.SCIMUser{}, err
	}
	if in.Active && !cur.Active {
		if err := s.enforceTenantActive(ctx, tenant); err != nil {
			return store.SCIMUser{}, err
		}
	}
	email := cur.Email
	if strings.TrimSpace(in.Email) != "" {
		if email, err = scimEmail(in); err != nil {
			return store.SCIMUser{}, err
		}
	}
	next := cur
	next.UserName, next.ExternalID, next.Email = strings.TrimSpace(in.UserName), in.ExternalID, email
	next.GivenName, next.FamilyName, next.DisplayName, next.Active = in.GivenName, in.FamilyName, in.DisplayName, in.Active
	rec, err := s.store.UpdateSCIMUser(ctx, next)
	if err != nil {
		if isUniqueViolation(err) {
			return store.SCIMUser{}, ErrConflict
		}
		return store.SCIMUser{}, fmt.Errorf("update scim user: %w", err)
	}
	switch {
	case in.Active && !cur.Active:
		err = s.scimActivate(ctx, tenant.ID, cur.UserID)
	case !in.Active && cur.Active:
		err = s.scimDeprovision(ctx, tenant.ID, cur.UserID)
	case in.Active:
		// Already active: make sure membership exists (it may have been
		// removed in Quill's UI; the IdP is the source of truth).
		err = s.scimActivate(ctx, tenant.ID, cur.UserID)
	}
	return rec, err
}

// SCIMDeleteUser deprovisions a person and forgets the record.
func (s *Service) SCIMDeleteUser(ctx context.Context, tenant db.Tenant, id uuid.UUID) error {
	cur, err := s.SCIMGetUser(ctx, tenant, id)
	if err != nil {
		return err
	}
	if err := s.scimDeprovision(ctx, tenant.ID, cur.UserID); err != nil {
		return err
	}
	return s.store.DeleteSCIMUser(ctx, tenant.ID, id)
}

func (s *Service) scimActivate(ctx context.Context, tenantID, userID uuid.UUID) error {
	added, err := s.store.AddTenantMemberIfAbsent(ctx, tenantID, userID)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if added {
		s.syncTeamSeatsBestEffort(ctx, tenantID)
	}
	return nil
}

func (s *Service) scimDeprovision(ctx context.Context, tenantID, userID uuid.UUID) error {
	removed, err := s.store.RemoveUserFromTenant(ctx, tenantID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if removed {
		s.syncTeamSeatsBestEffort(ctx, tenantID)
	}
	return nil
}

var usernameUnsafeRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// createProvisionedUser creates the account for a person who has never signed
// in. It has no auth identity until their first sign-in links it.
func (s *Service) createProvisionedUser(ctx context.Context, in SCIMUserInput, email string) (db.User, error) {
	base := strings.ToLower(email[:strings.IndexByte(email, '@')])
	base = strings.Trim(usernameUnsafeRe.ReplaceAllString(base, "-"), "-_")
	if len(base) < 2 || !validSlug(base) {
		base = "user"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = strings.TrimSpace(in.GivenName + " " + in.FamilyName)
	}
	for i := 0; i < 20; i++ {
		name := base
		if i > 0 {
			name = base + "-" + strconv.Itoa(i+1)
		}
		if i >= 10 {
			suffix, err := randomToken(4)
			if err != nil {
				return db.User{}, err
			}
			name = base + "-" + strings.ToLower(usernameUnsafeRe.ReplaceAllString(suffix, ""))
		}
		if display == "" {
			display = name
		}
		u, err := s.store.CreateUser(ctx, db.CreateUserParams{Username: name, Email: email, DisplayName: display, IsActive: true})
		if err == nil {
			return u, nil
		}
		if !isUniqueViolation(err) {
			return db.User{}, err
		}
		// The email may have been created concurrently: use that account.
		if existing, gerr := s.store.GetUserByEmail(ctx, email); gerr == nil {
			return existing, nil
		}
	}
	return db.User{}, fmt.Errorf("could not allocate a username for %s", email)
}

// ---- groups ------------------------------------------------------------------

// SCIMGroupView is a group with its provisioned members.
type SCIMGroupView struct {
	store.SCIMGroup
	Members []store.SCIMUser
}

var groupSlugUnsafeRe = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Service) scimGroupView(ctx context.Context, tenant db.Tenant, g store.SCIMGroup) (SCIMGroupView, error) {
	members, err := s.store.SCIMGroupMembers(ctx, tenant.ID, g.GroupID)
	if err != nil {
		return SCIMGroupView{}, fmt.Errorf("list group members: %w", err)
	}
	return SCIMGroupView{SCIMGroup: g, Members: members}, nil
}

// SCIMCreateGroup creates an IdP-owned Quill group (flat, depth 1).
func (s *Service) SCIMCreateGroup(ctx context.Context, tenant db.Tenant, displayName, externalID string, members []uuid.UUID) (SCIMGroupView, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 200 {
		return SCIMGroupView{}, fmt.Errorf("%w: displayName is required (max 200 chars)", ErrInvalidInput)
	}
	userIDs, err := s.scimMemberUserIDs(ctx, tenant, members)
	if err != nil {
		return SCIMGroupView{}, err
	}
	if existing, _, err := s.store.ListSCIMGroups(ctx, tenant.ID, store.SCIMUserFilter{Attr: "displayName", Value: displayName}, 0, 1); err != nil {
		return SCIMGroupView{}, err
	} else if len(existing) > 0 {
		return SCIMGroupView{}, ErrConflict
	}
	base := strings.Trim(groupSlugUnsafeRe.ReplaceAllString(strings.ToLower(displayName), "-"), "-")
	if base == "" {
		base = "group"
	}
	if len(base) > 55 {
		base = strings.Trim(base[:55], "-")
	}
	var group db.Group
	for i := 0; ; i++ {
		slug := base
		if i > 0 {
			slug = base + "-" + strconv.Itoa(i+1)
		}
		group, err = s.store.CreateGroup(ctx, db.CreateGroupParams{TenantID: tenant.ID, Slug: slug, Name: displayName, Depth: 1})
		if err == nil {
			break
		}
		if !isUniqueViolation(err) || i >= 50 {
			return SCIMGroupView{}, fmt.Errorf("create group: %w", err)
		}
	}
	rec, err := s.store.InsertSCIMGroup(ctx, store.SCIMGroup{TenantID: tenant.ID, GroupID: group.ID, DisplayName: displayName, ExternalID: externalID})
	if err != nil {
		_ = s.store.DeleteGroup(ctx, group.ID)
		if isUniqueViolation(err) {
			return SCIMGroupView{}, ErrConflict
		}
		return SCIMGroupView{}, fmt.Errorf("record scim group: %w", err)
	}
	for _, uid := range userIDs {
		if err := s.store.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: group.ID, UserID: uid}); err != nil {
			return SCIMGroupView{}, fmt.Errorf("add group member: %w", err)
		}
	}
	return s.scimGroupView(ctx, tenant, rec)
}

// SCIMGetGroup loads a group with its members.
func (s *Service) SCIMGetGroup(ctx context.Context, tenant db.Tenant, id uuid.UUID) (SCIMGroupView, error) {
	g, err := s.store.GetSCIMGroup(ctx, tenant.ID, id)
	if errors.Is(err, store.ErrSCIMNotFound) {
		return SCIMGroupView{}, ErrNotFound
	}
	if err != nil {
		return SCIMGroupView{}, err
	}
	return s.scimGroupView(ctx, tenant, g)
}

// SCIMListGroups pages groups; withMembers=false skips member expansion
// (excludedAttributes=members, which Entra ID uses).
func (s *Service) SCIMListGroups(ctx context.Context, tenant db.Tenant, f store.SCIMUserFilter, offset, limit int, withMembers bool) ([]SCIMGroupView, int, error) {
	groups, total, err := s.store.ListSCIMGroups(ctx, tenant.ID, f, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]SCIMGroupView, 0, len(groups))
	for _, g := range groups {
		if !withMembers {
			out = append(out, SCIMGroupView{SCIMGroup: g})
			continue
		}
		v, err := s.scimGroupView(ctx, tenant, g)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, nil
}

// SCIMGroupChange is a group update: nil fields stay as they are.
type SCIMGroupChange struct {
	DisplayName *string
	ExternalID  *string
	SetMembers  *[]uuid.UUID // replace the member list
	Add         []uuid.UUID
	Remove      []uuid.UUID
}

// SCIMUpdateGroup applies a PUT (SetMembers) or PATCH (Add/Remove) change.
func (s *Service) SCIMUpdateGroup(ctx context.Context, tenant db.Tenant, id uuid.UUID, ch SCIMGroupChange) (SCIMGroupView, error) {
	g, err := s.store.GetSCIMGroup(ctx, tenant.ID, id)
	if errors.Is(err, store.ErrSCIMNotFound) {
		return SCIMGroupView{}, ErrNotFound
	}
	if err != nil {
		return SCIMGroupView{}, err
	}
	if ch.DisplayName != nil || ch.ExternalID != nil {
		next := g
		if ch.DisplayName != nil {
			name := strings.TrimSpace(*ch.DisplayName)
			if name == "" || len(name) > 200 {
				return SCIMGroupView{}, fmt.Errorf("%w: displayName is required (max 200 chars)", ErrInvalidInput)
			}
			next.DisplayName = name
		}
		if ch.ExternalID != nil {
			next.ExternalID = *ch.ExternalID
		}
		if g, err = s.store.UpdateSCIMGroup(ctx, next); err != nil {
			if isUniqueViolation(err) {
				return SCIMGroupView{}, ErrConflict
			}
			return SCIMGroupView{}, fmt.Errorf("update scim group: %w", err)
		}
	}

	add, err := s.scimMemberUserIDs(ctx, tenant, ch.Add)
	if err != nil {
		return SCIMGroupView{}, err
	}
	remove, err := s.scimMemberUserIDs(ctx, tenant, ch.Remove)
	if err != nil {
		return SCIMGroupView{}, err
	}
	if ch.SetMembers != nil {
		want, err := s.scimMemberUserIDs(ctx, tenant, *ch.SetMembers)
		if err != nil {
			return SCIMGroupView{}, err
		}
		current, err := s.store.SCIMGroupMembers(ctx, tenant.ID, g.GroupID)
		if err != nil {
			return SCIMGroupView{}, err
		}
		keep := map[uuid.UUID]bool{}
		for _, u := range want {
			keep[u] = true
		}
		for _, m := range current {
			if !keep[m.UserID] {
				remove = append(remove, m.UserID)
			}
		}
		add = append(add, want...)
	}
	for _, uid := range remove {
		if err := s.store.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{GroupID: g.GroupID, UserID: uid}); err != nil {
			return SCIMGroupView{}, fmt.Errorf("remove group member: %w", err)
		}
	}
	for _, uid := range add {
		if err := s.store.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: g.GroupID, UserID: uid}); err != nil {
			return SCIMGroupView{}, fmt.Errorf("add group member: %w", err)
		}
	}
	return s.scimGroupView(ctx, tenant, g)
}

// SCIMDeleteGroup deletes an IdP-owned group (and its Quill group, with its
// project grants and memberships).
func (s *Service) SCIMDeleteGroup(ctx context.Context, tenant db.Tenant, id uuid.UUID) error {
	g, err := s.store.GetSCIMGroup(ctx, tenant.ID, id)
	if errors.Is(err, store.ErrSCIMNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.store.DeleteGroup(ctx, g.GroupID) // cascades scim_groups
}

// scimMemberUserIDs maps SCIM user ids to Quill user ids. Every id must belong
// to this tenant (an unknown id is an input error, so another tenant's ids
// can never be smuggled in). Deactivated people are skipped: a group
// membership would otherwise grant project access to someone deprovisioned.
func (s *Service) scimMemberUserIDs(ctx context.Context, tenant db.Tenant, ids []uuid.UUID) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		u, err := s.store.GetSCIMUser(ctx, tenant.ID, id)
		if errors.Is(err, store.ErrSCIMNotFound) {
			return nil, fmt.Errorf("%w: unknown member %s", ErrInvalidInput, id)
		}
		if err != nil {
			return nil, err
		}
		if u.Active {
			out = append(out, u.UserID)
		}
	}
	return out, nil
}
