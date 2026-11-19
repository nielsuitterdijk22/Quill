package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ProjectUserGrant is one direct project_members row with the person's identity.
type ProjectUserGrant struct {
	UserID      uuid.UUID
	Username    string
	DisplayName string
	Role        string
	Since       time.Time
}

// ProjectGroupGrant is one group_project_roles row with the group's identity.
type ProjectGroupGrant struct {
	GroupID uuid.UUID
	Slug    string
	Name    string
	Role    string
	Since   time.Time
}

// ListProjectUserGrants returns a project's direct grants, strongest first.
func (s *Store) ListProjectUserGrants(ctx context.Context, projectID uuid.UUID) ([]ProjectUserGrant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, pm.role, pm.created_at
		FROM project_members pm JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY CASE pm.role WHEN 'owner' THEN 0 WHEN 'member' THEN 1 ELSE 2 END, u.username`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectUserGrant
	for rows.Next() {
		var g ProjectUserGrant
		if err := rows.Scan(&g.UserID, &g.Username, &g.DisplayName, &g.Role, &g.Since); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListProjectGroupGrants returns the groups granted a role on a project.
func (s *Store) ListProjectGroupGrants(ctx context.Context, projectID uuid.UUID) ([]ProjectGroupGrant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.slug, g.name, gpr.role, gpr.created_at
		FROM group_project_roles gpr JOIN groups g ON g.id = gpr.group_id
		WHERE gpr.project_id = $1
		ORDER BY CASE gpr.role WHEN 'owner' THEN 0 WHEN 'member' THEN 1 ELSE 2 END, g.slug`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectGroupGrant
	for rows.Next() {
		var g ProjectGroupGrant
		if err := rows.Scan(&g.GroupID, &g.Slug, &g.Name, &g.Role, &g.Since); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CountProjectOwnerGrants counts the owner grants on a project — direct rows of
// current org members plus group owner grants — optionally ignoring one user's
// direct row (what would remain after revoking or demoting them).
func (s *Store) CountProjectOwnerGrants(ctx context.Context, projectID uuid.UUID, exceptUser uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM project_members pm
		     JOIN projects p ON p.id = pm.project_id
		     JOIN tenants t ON t.id = p.tenant_id
		    WHERE pm.project_id = $1 AND pm.role = 'owner' AND pm.user_id <> $2
		      AND (t.kind <> 'org' OR EXISTS (SELECT 1 FROM tenant_members tm WHERE tm.tenant_id = p.tenant_id AND tm.user_id = pm.user_id)))
		+ (SELECT count(*) FROM group_project_roles gpr WHERE gpr.project_id = $1 AND gpr.role = 'owner')`,
		projectID, exceptUser).Scan(&n)
	return n, err
}

// GetProjectMemberRole returns a person's direct role on a project (pgx.ErrNoRows if none).
func (s *Store) GetProjectMemberRole(ctx context.Context, projectID, userID uuid.UUID) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID).Scan(&role)
	return role, err
}

// GetGroupProjectRoleValue returns a group's role on a project (pgx.ErrNoRows if none).
func (s *Store) GetGroupProjectRoleValue(ctx context.Context, groupID, projectID uuid.UUID) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `SELECT role FROM group_project_roles WHERE group_id = $1 AND project_id = $2`, groupID, projectID).Scan(&role)
	return role, err
}

// IsOrgTenant reports whether the tenant is an organization (not a personal tenant).
func (s *Store) IsOrgTenant(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var kind string
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM tenants WHERE id = $1`, tenantID).Scan(&kind); err != nil {
		return false, err
	}
	return kind == "org", nil
}
