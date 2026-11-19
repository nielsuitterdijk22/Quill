package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Project access management. Project owners — and only they — decide who
// reaches a project: direct grants to organization members and role grants to
// groups. Org admins run the organization (members, groups) but have no
// standing access to its projects; they can add themselves as an owner, which
// is audited by the HTTP layer (break-glass).

// ProjectAccessUser is a person with a direct grant.
type ProjectAccessUser struct {
	UserID      uuid.UUID `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Since       time.Time `json:"since"`
}

// ProjectAccessGroup is a group with a role grant.
type ProjectAccessGroup struct {
	GroupID uuid.UUID `json:"groupId"`
	Slug    string    `json:"slug"`
	Name    string    `json:"name"`
	Role    string    `json:"role"`
	Since   time.Time `json:"since"`
}

// ProjectAccess is who can reach a project and why.
type ProjectAccess struct {
	Users  []ProjectAccessUser  `json:"users"`
	Groups []ProjectAccessGroup `json:"groups"`
}

func validProjectRole(role string) bool {
	return role == "owner" || role == "member" || role == "read"
}

// ownedOrgProject loads an organization project and requires the actor to be
// one of its owners. Personal projects are not shareable (no guests yet).
func (s *Service) ownedOrgProject(ctx context.Context, actor Actor, projectSlug string) (db.Project, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Project{}, err
	}
	isOrg, err := s.store.IsOrgTenant(ctx, project.TenantID)
	if err != nil {
		return db.Project{}, fmt.Errorf("lookup tenant: %w", err)
	}
	if !isOrg {
		return db.Project{}, fmt.Errorf("%w: personal projects cannot be shared", ErrInvalidInput)
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return db.Project{}, err
	}
	return project, nil
}

// ListProjectAccess lists direct and group grants (owners only).
func (s *Service) ListProjectAccess(ctx context.Context, actor Actor, projectSlug string) (ProjectAccess, error) {
	project, err := s.ownedOrgProject(ctx, actor, projectSlug)
	if err != nil {
		return ProjectAccess{}, err
	}
	users, err := s.store.ListProjectUserGrants(ctx, project.ID)
	if err != nil {
		return ProjectAccess{}, fmt.Errorf("list project users: %w", err)
	}
	groups, err := s.store.ListProjectGroupGrants(ctx, project.ID)
	if err != nil {
		return ProjectAccess{}, fmt.Errorf("list project groups: %w", err)
	}
	out := ProjectAccess{Users: []ProjectAccessUser{}, Groups: []ProjectAccessGroup{}}
	for _, u := range users {
		out.Users = append(out.Users, ProjectAccessUser{UserID: u.UserID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role, Since: u.Since})
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, ProjectAccessGroup{GroupID: g.GroupID, Slug: g.Slug, Name: g.Name, Role: g.Role, Since: g.Since})
	}
	return out, nil
}

// requireAnotherOwner refuses a change that would leave the project without an
// owner (ignoring exceptUser's direct row).
func (s *Service) requireAnotherOwner(ctx context.Context, projectID, exceptUser uuid.UUID) error {
	n, err := s.store.CountProjectOwnerGrants(ctx, projectID, exceptUser)
	if err != nil {
		return fmt.Errorf("count owners: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: a project needs at least one owner", ErrInvalidInput)
	}
	return nil
}

// SetProjectUserRole grants (or changes) an organization member's direct role.
func (s *Service) SetProjectUserRole(ctx context.Context, actor Actor, projectSlug, username, role string) error {
	if !validProjectRole(role) {
		return fmt.Errorf("%w: role must be owner, member or read", ErrInvalidInput)
	}
	project, err := s.ownedOrgProject(ctx, actor, projectSlug)
	if err != nil {
		return err
	}
	user, err := s.store.GetUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	if _, err := s.store.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: project.TenantID, UserID: user.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: only members of the organization can be given project access", ErrInvalidInput)
		}
		return fmt.Errorf("check membership: %w", err)
	}
	if role != "owner" {
		// Demoting an owner must leave another owner behind.
		if cur, err := s.store.GetProjectMemberRole(ctx, project.ID, user.ID); err == nil && cur == "owner" {
			if err := s.requireAnotherOwner(ctx, project.ID, user.ID); err != nil {
				return err
			}
		}
	}
	return s.store.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: user.ID, Role: role})
}

// RemoveProjectUser revokes a person's direct grant.
func (s *Service) RemoveProjectUser(ctx context.Context, actor Actor, projectSlug string, userID uuid.UUID) error {
	project, err := s.ownedOrgProject(ctx, actor, projectSlug)
	if err != nil {
		return err
	}
	if err := s.requireAnotherOwner(ctx, project.ID, userID); err != nil {
		return err
	}
	return s.store.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{ProjectID: project.ID, UserID: userID})
}

func (s *Service) projectGroup(ctx context.Context, project db.Project, groupSlug string) (db.Group, error) {
	g, err := s.store.GetGroupBySlug(ctx, db.GetGroupBySlugParams{TenantID: project.TenantID, Slug: strings.TrimSpace(groupSlug)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Group{}, ErrNotFound
	}
	if err != nil {
		return db.Group{}, fmt.Errorf("lookup group: %w", err)
	}
	return g, nil
}

// SetProjectGroupRole grants a group of the project's organization a role.
func (s *Service) SetProjectGroupRole(ctx context.Context, actor Actor, projectSlug, groupSlug, role string) error {
	if !validProjectRole(role) {
		return fmt.Errorf("%w: role must be owner, member or read", ErrInvalidInput)
	}
	project, err := s.ownedOrgProject(ctx, actor, projectSlug)
	if err != nil {
		return err
	}
	g, err := s.projectGroup(ctx, project, groupSlug)
	if err != nil {
		return err
	}
	return s.store.SetGroupProjectRole(ctx, db.SetGroupProjectRoleParams{GroupID: g.ID, ProjectID: project.ID, Role: role})
}

// RemoveProjectGroup revokes a group's grant.
func (s *Service) RemoveProjectGroup(ctx context.Context, actor Actor, projectSlug, groupSlug string) error {
	project, err := s.ownedOrgProject(ctx, actor, projectSlug)
	if err != nil {
		return err
	}
	g, err := s.projectGroup(ctx, project, groupSlug)
	if err != nil {
		return err
	}
	// Revoking an owner group must leave an owner behind.
	if cur, err := s.store.GetGroupProjectRoleValue(ctx, g.ID, project.ID); err == nil && cur == "owner" {
		n, err := s.store.CountProjectOwnerGrants(ctx, project.ID, uuid.Nil)
		if err != nil {
			return err
		}
		if n <= 1 {
			return fmt.Errorf("%w: a project needs at least one owner", ErrInvalidInput)
		}
	}
	return s.store.RemoveGroupProjectRole(ctx, db.RemoveGroupProjectRoleParams{GroupID: g.ID, ProjectID: project.ID})
}

// SelfGrantProjectOwner is the org admin's break-glass: they add themselves as
// an owner of one of the organization's projects (the HTTP layer audits it).
// Only organization admins may; ordinary members and outsiders get ErrForbidden.
func (s *Service) SelfGrantProjectOwner(ctx context.Context, actor Actor, projectSlug string) error {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return err
	}
	isOrg, err := s.store.IsOrgTenant(ctx, project.TenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	if !isOrg {
		return fmt.Errorf("%w: personal projects cannot be shared", ErrInvalidInput)
	}
	m, err := s.store.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: project.TenantID, UserID: actor.UserID})
	if err != nil || m.Role != "admin" {
		return ErrForbidden
	}
	return s.store.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: actor.UserID, Role: "owner"})
}
