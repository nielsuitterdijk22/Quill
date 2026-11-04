package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

type groupResponse struct {
	ID       string  `json:"id"`
	Slug     string  `json:"slug"`
	Name     string  `json:"name"`
	Depth    int32   `json:"depth"`
	ParentID *string `json:"parentId,omitempty"`
}

type createGroupRequest struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	ParentSlug string `json:"parentSlug,omitempty"`
}

type updateGroupRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type groupMemberResponse struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Since       string `json:"since"`
}

type addGroupMemberRequest struct {
	UserID string `json:"userId"`
}

type setGroupProjectRoleRequest struct {
	Role string `json:"role"`
}

func handleGroupsList(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groups, err := s.platform.ListGroups(r.Context(), actor, orgSlug)
		if err != nil {
			s.writePlatformError(w, err, "could not list groups")
			return
		}
		out := make([]groupResponse, 0, len(groups))
		for _, g := range groups {
			resp := groupResponse{ID: g.ID.String(), Slug: g.Slug, Name: g.Name, Depth: g.Depth}
			if g.ParentID != nil {
				p := g.ParentID.String()
				resp.ParentID = &p
			}
			out = append(out, resp)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"groups": out})
	}
}

func handleGroupsCreate(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		var req createGroupRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		group, err := s.platform.CreateGroup(r.Context(), actor, orgSlug, req.Slug, req.Name, req.ParentSlug)
		if err != nil {
			s.writePlatformError(w, err, "could not create group")
			return
		}
		s.logAudit(r, "group.created", "group", group.ID.String(), map[string]any{
			"org": orgSlug, "slug": group.Slug, "parentSlug": req.ParentSlug,
		})
		resp := groupResponse{ID: group.ID.String(), Slug: group.Slug, Name: group.Name, Depth: group.Depth}
		if group.ParentID != nil {
			p := group.ParentID.String()
			resp.ParentID = &p
		}
		httpx.JSON(w, http.StatusCreated, map[string]any{"group": resp})
	}
}

func handleGroupsUpdate(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groupSlug := chi.URLParam(r, "group")
		var req updateGroupRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		group, err := s.platform.UpdateGroup(r.Context(), actor, orgSlug, groupSlug, req.Name, req.Slug)
		if err != nil {
			s.writePlatformError(w, err, "could not update group")
			return
		}
		s.logAudit(r, "group.updated", "group", group.ID.String(), map[string]any{
			"org": orgSlug, "slug": group.Slug,
		})
		httpx.JSON(w, http.StatusOK, map[string]any{"group": groupResponse{ID: group.ID.String(), Slug: group.Slug, Name: group.Name, Depth: group.Depth}})
	}
}

func handleGroupsDelete(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groupSlug := chi.URLParam(r, "group")
		if err := s.platform.DeleteGroup(r.Context(), actor, orgSlug, groupSlug); err != nil {
			s.writePlatformError(w, err, "could not delete group")
			return
		}
		s.logAudit(r, "group.deleted", "group", groupSlug, map[string]any{"org": orgSlug})
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleGroupMembersList(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		members, err := s.platform.ListGroupMembers(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "group"))
		if err != nil {
			s.writePlatformError(w, err, "could not list group members")
			return
		}
		out := make([]groupMemberResponse, 0, len(members))
		for _, m := range members {
			out = append(out, groupMemberResponse{
				UserID:      m.UserID.String(),
				Username:    m.Username,
				Email:       m.Email,
				DisplayName: m.DisplayName,
				Since:       m.Since.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
	}
}

func handleGroupMembersAdd(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		var req addGroupMemberRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		uid, err := uuid.Parse(req.UserID)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid", "invalid user id")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groupSlug := chi.URLParam(r, "group")
		if err := s.platform.AddGroupMember(r.Context(), actor, orgSlug, groupSlug, uid); err != nil {
			s.writePlatformError(w, err, "could not add group member")
			return
		}
		s.logAudit(r, "group.member.added", "group", groupSlug, map[string]any{"org": orgSlug, "userId": uid.String()})
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleGroupMembersRemove(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		uid, err := uuid.Parse(chi.URLParam(r, "userId"))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid", "invalid user id")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groupSlug := chi.URLParam(r, "group")
		if err := s.platform.RemoveGroupMember(r.Context(), actor, orgSlug, groupSlug, uid); err != nil {
			s.writePlatformError(w, err, "could not remove group member")
			return
		}
		s.logAudit(r, "group.member.removed", "group", groupSlug, map[string]any{"org": orgSlug, "userId": uid.String()})
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleGroupProjectRoleSet(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		var req setGroupProjectRoleRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groupSlug := chi.URLParam(r, "group")
		projectSlug := chi.URLParam(r, "project")
		if err := s.platform.SetGroupProjectRole(r.Context(), actor, orgSlug, groupSlug, projectSlug, req.Role); err != nil {
			s.writePlatformError(w, err, "could not grant group project role")
			return
		}
		s.logAudit(r, "group.project_role_set", "group", groupSlug, map[string]any{
			"org": orgSlug, "project": projectSlug, "role": req.Role,
		})
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleGroupProjectRoleRemove(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		groupSlug := chi.URLParam(r, "group")
		projectSlug := chi.URLParam(r, "project")
		if err := s.platform.RemoveGroupProjectRole(r.Context(), actor, orgSlug, groupSlug, projectSlug); err != nil {
			s.writePlatformError(w, err, "could not revoke group project role")
			return
		}
		s.logAudit(r, "group.project_role_removed", "group", groupSlug, map[string]any{
			"org": orgSlug, "project": projectSlug,
		})
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
