package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// MaxGroupDepth is the deepest allowed nesting for groups inside a tenant
// (1 = root/department, up to 5). Mirrors the CHECK constraint on groups.depth.
const MaxGroupDepth = 5

var groupSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// GroupView is the API-facing shape of a group.
type GroupView struct {
	ID       uuid.UUID  `json:"id"`
	Slug     string     `json:"slug"`
	Name     string     `json:"name"`
	Depth    int32      `json:"depth"`
	ParentID *uuid.UUID `json:"parentId,omitempty"`
}

// GroupMember is one user's membership in a group.
type GroupMember struct {
	UserID      uuid.UUID `json:"userId"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Since       time.Time `json:"since"`
}

func groupView(g db.Group) GroupView {
	v := GroupView{ID: g.ID, Slug: g.Slug, Name: g.Name, Depth: g.Depth}
	if g.ParentID.Valid {
		id := uuid.UUID(g.ParentID.Bytes)
		v.ParentID = &id
	}
	return v
}

// resolveGroup loads a group by slug within the org tenant and authorizes the
// actor as a tenant member (any member may read groups).
func (s *Service) resolveGroup(ctx context.Context, actor Actor, orgSlug, groupSlug string) (db.Tenant, db.Group, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return db.Tenant{}, db.Group{}, err
	}
	if err := s.authorizeTenantMember(ctx, actor, tenant.ID); err != nil {
		return db.Tenant{}, db.Group{}, err
	}
	group, err := s.store.GetGroupBySlug(ctx, db.GetGroupBySlugParams{TenantID: tenant.ID, Slug: strings.TrimSpace(groupSlug)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Tenant{}, db.Group{}, ErrNotFound
	}
	if err != nil {
		return db.Tenant{}, db.Group{}, fmt.Errorf("lookup group: %w", err)
	}
	return tenant, group, nil
}

// CreateGroup creates a group under the org tenant (root when parentSlug is
// empty, otherwise nested under that parent). Only tenant admins may create
// groups. Depth is parent.depth+1 and must not exceed MaxGroupDepth.
func (s *Service) CreateGroup(ctx context.Context, actor Actor, orgSlug, slug, name, parentSlug string) (GroupView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return GroupView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return GroupView{}, err
	}
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	if !groupSlugRe.MatchString(slug) || name == "" {
		return GroupView{}, ErrInvalidInput
	}

	var parentID pgtype.UUID
	depth := int32(1)
	if parentSlug != "" {
		parent, err := s.store.GetGroupBySlug(ctx, db.GetGroupBySlugParams{TenantID: tenant.ID, Slug: parentSlug})
		if errors.Is(err, pgx.ErrNoRows) {
			return GroupView{}, ErrNotFound
		}
		if err != nil {
			return GroupView{}, fmt.Errorf("lookup parent group: %w", err)
		}
		depth = parent.Depth + 1
		if depth > MaxGroupDepth {
			return GroupView{}, fmt.Errorf("%w: max group depth is %d", ErrInvalidInput, MaxGroupDepth)
		}
		parentID = pgtype.UUID{Bytes: [16]byte(parent.ID), Valid: true}
	}

	group, err := s.store.CreateGroup(ctx, db.CreateGroupParams{
		TenantID: tenant.ID,
		ParentID: parentID,
		Slug:     slug,
		Name:     name,
		Depth:    depth,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return GroupView{}, ErrConflict
		}
		return GroupView{}, fmt.Errorf("create group: %w", err)
	}
	return groupView(group), nil
}

// ListGroups returns the org's whole group tree (flat list with parent links;
// the UI renders it as a tree).
func (s *Service) ListGroups(ctx context.Context, actor Actor, orgSlug string) ([]GroupView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeTenantMember(ctx, actor, tenant.ID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListGroupsByTenant(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	out := make([]GroupView, 0, len(rows))
	for _, g := range rows {
		out = append(out, groupView(g))
	}
	return out, nil
}

// UpdateGroup renames/re-slugs a group (tenant admin only).
func (s *Service) UpdateGroup(ctx context.Context, actor Actor, orgSlug, groupSlug, name, newSlug string) (GroupView, error) {
	tenant, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return GroupView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return GroupView{}, err
	}
	newSlug = strings.TrimSpace(newSlug)
	name = strings.TrimSpace(name)
	if !groupSlugRe.MatchString(newSlug) || name == "" {
		return GroupView{}, ErrInvalidInput
	}
	updated, err := s.store.UpdateGroup(ctx, db.UpdateGroupParams{ID: group.ID, Name: name, Slug: newSlug})
	if err != nil {
		if isUniqueViolation(err) {
			return GroupView{}, ErrConflict
		}
		return GroupView{}, fmt.Errorf("update group: %w", err)
	}
	return groupView(updated), nil
}

// DeleteGroup removes a group; memberships and project grants cascade. Child
// groups are deleted by the self-referencing ON DELETE CASCADE.
func (s *Service) DeleteGroup(ctx context.Context, actor Actor, orgSlug, groupSlug string) error {
	tenant, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	if err := s.store.DeleteGroup(ctx, group.ID); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

// AddGroupMember adds a user to a group (tenant admin only).
func (s *Service) AddGroupMember(ctx context.Context, actor Actor, orgSlug, groupSlug string, userID uuid.UUID) error {
	_, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return err
	}
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	// The target must exist and (checked below) be a member of the org.
	if _, err := s.store.GetUserByID(ctx, userID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	if _, err := s.store.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: only members of the organization can join its groups", ErrInvalidInput)
	} else if err != nil {
		return fmt.Errorf("lookup org member: %w", err)
	}
	if err := s.store.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: group.ID, UserID: userID}); err != nil {
		return fmt.Errorf("add group member: %w", err)
	}
	return nil
}

// RemoveGroupMember removes a user from a group (tenant admin only).
func (s *Service) RemoveGroupMember(ctx context.Context, actor Actor, orgSlug, groupSlug string, userID uuid.UUID) error {
	tenant, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	if err := s.store.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{GroupID: group.ID, UserID: userID}); err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	return nil
}

// ListGroupMembers returns the direct members of a group (any tenant member).
func (s *Service) ListGroupMembers(ctx context.Context, actor Actor, orgSlug, groupSlug string) ([]GroupMember, error) {
	_, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListGroupMembers(ctx, group.ID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	out := make([]GroupMember, 0, len(rows))
	for _, r := range rows {
		out = append(out, GroupMember{
			UserID:      r.ID,
			Username:    r.Username,
			Email:       r.Email,
			DisplayName: r.DisplayName,
			Since:       r.MemberSince,
		})
	}
	return out, nil
}

// SetGroupProjectRole grants (or updates) a group's role on a project
// (tenant admin only). role is 'owner' | 'member'.
func (s *Service) SetGroupProjectRole(ctx context.Context, actor Actor, orgSlug, groupSlug, projectSlug, role string) error {
	tenant, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	if role != "owner" && role != "member" {
		return ErrInvalidInput
	}
	project, err := s.tenantProject(ctx, tenant.ID, projectSlug)
	if err != nil {
		return err
	}
	if err := s.store.SetGroupProjectRole(ctx, db.SetGroupProjectRoleParams{
		GroupID:   group.ID,
		ProjectID: project.ID,
		Role:      role,
	}); err != nil {
		return fmt.Errorf("set group project role: %w", err)
	}
	return nil
}

// RemoveGroupProjectRole revokes a group's role on a project.
func (s *Service) RemoveGroupProjectRole(ctx context.Context, actor Actor, orgSlug, groupSlug, projectSlug string) error {
	tenant, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	project, err := s.tenantProject(ctx, tenant.ID, projectSlug)
	if err != nil {
		return err
	}
	if err := s.store.RemoveGroupProjectRole(ctx, db.RemoveGroupProjectRoleParams{GroupID: group.ID, ProjectID: project.ID}); err != nil {
		return fmt.Errorf("remove group project role: %w", err)
	}
	return nil
}

// effectiveProjectRole returns the actor's effective role on a project,
// combining their direct membership with roles inherited from groups
// (including ancestor groups). ” means no access.
func (s *Service) effectiveProjectRole(ctx context.Context, actor Actor, projectID uuid.UUID) (string, error) {
	if actor.IsAdmin {
		return "owner", nil
	}
	role, err := s.store.GetEffectiveProjectRole(ctx, db.GetEffectiveProjectRoleParams{
		ProjectID: projectID,
		UserID:    actor.UserID,
	})
	if err != nil {
		return "", fmt.Errorf("effective project role: %w", err)
	}
	return role, nil
}

// GroupProjectRole is one project a group has a role on.
type GroupProjectRole struct {
	ProjectSlug string
	Role        string
	Since       time.Time
}

// ListGroupProjectRoles returns the projects a group is granted (any tenant
// member may read it, like the group tree).
func (s *Service) ListGroupProjectRoles(ctx context.Context, actor Actor, orgSlug, groupSlug string) ([]GroupProjectRole, error) {
	_, group, err := s.resolveGroup(ctx, actor, orgSlug, groupSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListGroupProjectRoles(ctx, group.ID)
	if err != nil {
		return nil, fmt.Errorf("list group project roles: %w", err)
	}
	out := make([]GroupProjectRole, 0, len(rows))
	for _, r := range rows {
		out = append(out, GroupProjectRole{ProjectSlug: r.ProjectSlug, Role: r.Role, Since: r.CreatedAt})
	}
	return out, nil
}

// OrgProjectRef is a project in an organization (for pickers).
type OrgProjectRef struct {
	Slug string
	Name string
}

// ListOrgProjects returns an organization's projects (tenant members).
func (s *Service) ListOrgProjects(ctx context.Context, actor Actor, orgSlug string) ([]OrgProjectRef, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeTenantMember(ctx, actor, tenant.ID); err != nil {
		return nil, err
	}
	projects, err := s.store.ListProjectsByTenantID(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]OrgProjectRef, 0, len(projects))
	for _, p := range projects {
		out = append(out, OrgProjectRef{Slug: p.Slug, Name: p.Name})
	}
	return out, nil
}

// tenantProject resolves a project by slug WITHIN a tenant: a project of any
// other org (or a personal project) answers ErrNotFound, exactly like a missing
// one. Project slugs are global, so every org-admin operation that takes a
// project slug must go through this — SetGroupProjectRole once looked the slug
// up globally, letting an admin of ANY org (orgs are self-service) grant their
// own group `owner` on another org's or person's private project.
func (s *Service) tenantProject(ctx context.Context, tenantID uuid.UUID, slug string) (db.Project, error) {
	project, err := s.store.GetProjectBySlug(ctx, strings.TrimSpace(slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Project{}, ErrNotFound
	}
	if err != nil {
		return db.Project{}, fmt.Errorf("lookup project: %w", err)
	}
	if project.TenantID != tenantID {
		return db.Project{}, ErrNotFound
	}
	return project, nil
}
