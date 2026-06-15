package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// CreateTeamInput is the payload for creating a team within an organization.
type CreateTeamInput struct {
	Slug        string
	Name        string
	Description string
}

// validTeamRole reports whether role is one Quill recognises for team members.
func validTeamRole(role string) bool {
	switch role {
	case "maintainer", "member":
		return true
	default:
		return false
	}
}

// ListTeams returns the teams in an org for an authorized member, ordered by slug.
func (s *Service) ListTeams(ctx context.Context, actor Actor, orgSlug string) (db.Organization, []db.Team, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.Organization{}, nil, err
	}
	if err := s.authorizeOrgMember(ctx, actor, org.ID); err != nil {
		return db.Organization{}, nil, err
	}
	teams, err := s.store.ListTeamsByOrg(ctx, org.ID)
	if err != nil {
		return db.Organization{}, nil, fmt.Errorf("list teams: %w", err)
	}
	return org, teams, nil
}

// CreateTeam adds a team to an org. Only org owners (or platform admins) may
// create teams. The slug must be well-formed and unique within the org.
func (s *Service) CreateTeam(ctx context.Context, actor Actor, orgSlug string, in CreateTeamInput) (db.Team, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.Team{}, err
	}
	if err := s.authorizeOrgAdmin(ctx, actor, org.ID); err != nil {
		return db.Team{}, err
	}

	slug := normalizeSlug(in.Slug)
	name := strings.TrimSpace(in.Name)
	if !validSlug(slug) {
		return db.Team{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
	}
	if name == "" {
		name = slug
	}

	team, err := s.store.CreateTeam(ctx, db.CreateTeamParams{
		OrgID:       org.ID,
		Slug:        slug,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.Team{}, ErrConflict
		}
		return db.Team{}, fmt.Errorf("create team: %w", err)
	}
	return team, nil
}

// GetTeam returns a team and its members for an authorized org member.
func (s *Service) GetTeam(ctx context.Context, actor Actor, orgSlug, teamSlug string) (db.Organization, db.Team, []db.ListTeamMembersRow, error) {
	org, team, err := s.teamBySlug(ctx, actor, orgSlug, teamSlug)
	if err != nil {
		return db.Organization{}, db.Team{}, nil, err
	}
	members, err := s.store.ListTeamMembers(ctx, team.ID)
	if err != nil {
		return db.Organization{}, db.Team{}, nil, fmt.Errorf("list team members: %w", err)
	}
	return org, team, members, nil
}

// AddTeamMember adds (or updates the role of) a user in a team by username. Only
// org owners (or platform admins) may manage team membership. A team member must
// also belong to the org, so the user is added to the org roster if missing.
func (s *Service) AddTeamMember(ctx context.Context, actor Actor, orgSlug, teamSlug, username, role string) error {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeOrgAdmin(ctx, actor, org.ID); err != nil {
		return err
	}
	team, err := s.store.GetTeamBySlug(ctx, db.GetTeamBySlugParams{OrgID: org.ID, Lower: teamSlug})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("get team: %w", err)
	}

	if role == "" {
		role = "member"
	}
	if !validTeamRole(role) {
		return fmt.Errorf("%w: role must be 'maintainer' or 'member'", ErrInvalidInput)
	}

	user, err := s.store.GetUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: no user with that username", ErrInvalidInput)
	} else if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}

	return s.store.InTx(ctx, func(q *db.Queries) error {
		// A team member is implicitly an org member; ensure the org roster has the
		// user without downgrading an existing owner.
		members, err := q.ListOrgMembers(ctx, org.ID)
		if err != nil {
			return err
		}
		isOrgMember := false
		for _, m := range members {
			if m.ID == user.ID {
				isOrgMember = true
				break
			}
		}
		if !isOrgMember {
			if err := q.AddOrgMember(ctx, db.AddOrgMemberParams{OrgID: org.ID, UserID: user.ID, Role: "member"}); err != nil {
				return err
			}
		}
		return q.AddTeamMember(ctx, db.AddTeamMemberParams{TeamID: team.ID, UserID: user.ID, Role: role})
	})
}

// RemoveTeamMember removes a user from a team by user ID. Only org owners (or
// platform admins) may manage team membership.
func (s *Service) RemoveTeamMember(ctx context.Context, actor Actor, orgSlug, teamSlug string, userID uuid.UUID) error {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeOrgAdmin(ctx, actor, org.ID); err != nil {
		return err
	}
	team, err := s.store.GetTeamBySlug(ctx, db.GetTeamBySlugParams{OrgID: org.ID, Lower: teamSlug})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("get team: %w", err)
	}
	if err := s.store.RemoveTeamMember(ctx, db.RemoveTeamMemberParams{TeamID: team.ID, UserID: userID}); err != nil {
		return fmt.Errorf("remove team member: %w", err)
	}
	return nil
}

// ListMyTeams returns every team the actor belongs to across all organizations.
func (s *Service) ListMyTeams(ctx context.Context, actor Actor) ([]db.ListTeamsByUserRow, error) {
	teams, err := s.store.ListTeamsByUser(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list user teams: %w", err)
	}
	return teams, nil
}

// teamBySlug loads an org and one of its teams after authorizing the actor as a
// member of the org.
func (s *Service) teamBySlug(ctx context.Context, actor Actor, orgSlug, teamSlug string) (db.Organization, db.Team, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.Organization{}, db.Team{}, err
	}
	if err := s.authorizeOrgMember(ctx, actor, org.ID); err != nil {
		return db.Organization{}, db.Team{}, err
	}
	team, err := s.store.GetTeamBySlug(ctx, db.GetTeamBySlugParams{OrgID: org.ID, Lower: teamSlug})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Organization{}, db.Team{}, ErrNotFound
	} else if err != nil {
		return db.Organization{}, db.Team{}, fmt.Errorf("get team: %w", err)
	}
	return org, team, nil
}
