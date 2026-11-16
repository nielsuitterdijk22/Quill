package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// SCIM 2.0 service provider (RFC 7643 schemas, RFC 7644 protocol) at
// /scim/v2, authenticated by an org's provisioning token (Bearer qsc_…).
// Shapes follow what Okta and Microsoft Entra ID send: string booleans
// ("False"), case-insensitive ops, value-object PATCHes without a path, and
// members[value eq "…"] removal paths. Unknown user attributes (title,
// addresses, …) are accepted and ignored, as IdPs push many we don't store.

const (
	scimUserSchema   = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema  = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimListSchema   = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrorSchema  = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimPatchSchema  = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimMaxPageSize  = 200
	scimDefaultCount = 100
)

type scimTenantKey struct{}

func (s *Server) scimRoutes(r chi.Router) {
	r.Use(s.scimAuth)
	r.Get("/ServiceProviderConfig", s.scimServiceProviderConfig)
	r.Get("/ResourceTypes", s.scimResourceTypes)
	r.Get("/Schemas", s.scimSchemas)
	r.Get("/Users", s.scimListUsers)
	r.Post("/Users", s.scimCreateUser)
	r.Get("/Users/{id}", s.scimGetUser)
	r.Put("/Users/{id}", s.scimPutUser)
	r.Patch("/Users/{id}", s.scimPatchUser)
	r.Delete("/Users/{id}", s.scimDeleteUser)
	r.Get("/Groups", s.scimListGroups)
	r.Post("/Groups", s.scimCreateGroup)
	r.Get("/Groups/{id}", s.scimGetGroup)
	r.Put("/Groups/{id}", s.scimPutGroup)
	r.Patch("/Groups/{id}", s.scimPatchGroup)
	r.Delete("/Groups/{id}", s.scimDeleteGroup)
}

func (s *Server) scimAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			scimError(w, http.StatusUnauthorized, "", "a provisioning token is required (Authorization: Bearer qsc_…)")
			return
		}
		tenant, err := s.platform.SCIMAuthenticate(r.Context(), raw)
		if err != nil {
			if errors.Is(err, platform.ErrForbidden) {
				scimError(w, http.StatusUnauthorized, "", "invalid or revoked provisioning token")
				return
			}
			s.logger.Error("scim auth failed", "error", err)
			scimError(w, http.StatusInternalServerError, "", "internal error")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scimTenantKey{}, tenant)))
	})
}

func scimTenant(r *http.Request) db.Tenant {
	t, _ := r.Context().Value(scimTenantKey{}).(db.Tenant)
	return t
}

// ---- wire types ----------------------------------------------------------------

type scimMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location"`
}

type scimName struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Formatted  string `json:"formatted,omitempty"`
}

type scimEmailOut struct {
	Value   string `json:"value"`
	Type    string `json:"type"`
	Primary bool   `json:"primary"`
}

type scimUserOut struct {
	Schemas     []string       `json:"schemas"`
	ID          string         `json:"id"`
	ExternalID  string         `json:"externalId,omitempty"`
	UserName    string         `json:"userName"`
	Name        scimName       `json:"name"`
	DisplayName string         `json:"displayName,omitempty"`
	Emails      []scimEmailOut `json:"emails,omitempty"`
	Active      bool           `json:"active"`
	Meta        scimMeta       `json:"meta"`
}

type scimMemberRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

type scimGroupOut struct {
	Schemas     []string        `json:"schemas"`
	ID          string          `json:"id"`
	ExternalID  string          `json:"externalId,omitempty"`
	DisplayName string          `json:"displayName"`
	Members     []scimMemberRef `json:"members,omitempty"`
	Meta        scimMeta        `json:"meta"`
}

type scimList struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []any    `json:"Resources"`
}

func scimBase(r *http.Request) string {
	scheme := "https"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	} else if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.")) {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/scim/v2"
}

func userOut(r *http.Request, u store.SCIMUser) scimUserOut {
	out := scimUserOut{
		Schemas: []string{scimUserSchema}, ID: u.ID.String(), ExternalID: u.ExternalID, UserName: u.UserName,
		Name:        scimName{GivenName: u.GivenName, FamilyName: u.FamilyName, Formatted: strings.TrimSpace(u.GivenName + " " + u.FamilyName)},
		DisplayName: u.DisplayName, Active: u.Active,
		Meta: scimMeta{ResourceType: "User", Created: u.CreatedAt, LastModified: u.UpdatedAt, Location: scimBase(r) + "/Users/" + u.ID.String()},
	}
	if u.Email != "" {
		out.Emails = []scimEmailOut{{Value: u.Email, Type: "work", Primary: true}}
	}
	return out
}

func groupOut(r *http.Request, g platform.SCIMGroupView) scimGroupOut {
	out := scimGroupOut{
		Schemas: []string{scimGroupSchema}, ID: g.ID.String(), ExternalID: g.ExternalID, DisplayName: g.DisplayName,
		Meta: scimMeta{ResourceType: "Group", Created: g.CreatedAt, LastModified: g.UpdatedAt, Location: scimBase(r) + "/Groups/" + g.ID.String()},
	}
	for _, m := range g.Members {
		out.Members = append(out.Members, scimMemberRef{Value: m.ID.String(), Display: m.UserName, Ref: scimBase(r) + "/Users/" + m.ID.String()})
	}
	return out
}

func scimJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func scimError(w http.ResponseWriter, status int, scimType, detail string) {
	body := map[string]any{"schemas": []string{scimErrorSchema}, "status": strconv.Itoa(status), "detail": detail}
	if scimType != "" {
		body["scimType"] = scimType
	}
	scimJSON(w, status, body)
}

func (s *Server) scimPlatformError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		scimError(w, http.StatusNotFound, "", "resource not found")
	case errors.Is(err, platform.ErrConflict):
		scimError(w, http.StatusConflict, "uniqueness", "a resource with that userName/displayName already exists")
	case errors.Is(err, platform.ErrInvalidInput):
		scimError(w, http.StatusBadRequest, "invalidValue", err.Error())
	case errors.Is(err, platform.ErrLimitReached):
		scimError(w, http.StatusForbidden, "", err.Error())
	default:
		s.logger.Error("scim operation failed", "error", err)
		scimError(w, http.StatusInternalServerError, "", "internal error")
	}
}

// scimAudit stamps provisioning changes into the org's audit log; the actor
// is the IdP ("scim"), not a user.
func (s *Server) scimAudit(r *http.Request, action, targetID string, meta map[string]any) {
	raw, _ := json.Marshal(meta)
	t := scimTenant(r)
	if _, err := s.store.InsertAuditLog(r.Context(), db.InsertAuditLogParams{
		Action: action, TargetType: "scim", TargetID: targetID, Metadata: raw, IPAddress: r.RemoteAddr,
		ActorUsername: "scim", TenantID: uuid.NullUUID{UUID: t.ID, Valid: true},
	}); err != nil {
		s.logger.Warn("audit log write failed", "action", action, "error", err)
	}
}

func readSCIMBody(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalidSyntax", "could not read body")
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		scimError(w, http.StatusBadRequest, "invalidSyntax", "malformed JSON")
		return false
	}
	return true
}

func scimID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusNotFound, "", "resource not found")
		return uuid.Nil, false
	}
	return id, true
}

// ---- input parsing ---------------------------------------------------------------

// scimBool accepts JSON booleans and the strings Entra ID sends ("True").
func scimBool(v any) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func scimString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// primaryEmail picks emails[primary] (else the first) from a SCIM emails value.
func primaryEmail(v any) string {
	list, _ := v.([]any)
	first := ""
	for _, e := range list {
		m, _ := e.(map[string]any)
		val := scimString(m["value"])
		if first == "" {
			first = val
		}
		if p, _ := scimBool(m["primary"]); p && val != "" {
			return val
		}
	}
	return first
}

// applyUserAttrs merges a SCIM attribute map (a full resource, or a PATCH
// value object) into in. Keys are matched case-insensitively and may be
// dotted ("name.givenName"), as Entra ID sends in path-less PATCHes.
func applyUserAttrs(in *platform.SCIMUserInput, attrs map[string]any) error {
	for k, v := range attrs {
		if err := applyUserAttr(in, k, v); err != nil {
			return err
		}
	}
	return nil
}

var emailPathRe = regexp.MustCompile(`(?i)^emails(\[.*\])?(\.value)?$`)

func applyUserAttr(in *platform.SCIMUserInput, path string, v any) error {
	p := strings.ToLower(strings.TrimSpace(path))
	p = strings.TrimPrefix(p, strings.ToLower(scimUserSchema)+":")
	switch {
	case p == "active":
		b, ok := scimBool(v)
		if !ok {
			return fmt.Errorf("%w: active must be a boolean", platform.ErrInvalidInput)
		}
		in.Active = b
	case p == "username":
		in.UserName = scimString(v)
	case p == "externalid":
		in.ExternalID = scimString(v)
	case p == "displayname":
		in.DisplayName = scimString(v)
	case p == "name":
		m, _ := v.(map[string]any)
		for k, nv := range m {
			if err := applyUserAttr(in, "name."+k, nv); err != nil {
				return err
			}
		}
	case p == "name.givenname":
		in.GivenName = scimString(v)
	case p == "name.familyname":
		in.FamilyName = scimString(v)
	case p == "emails":
		if e := primaryEmail(v); e != "" {
			in.Email = e
		}
	case emailPathRe.MatchString(p):
		if s := scimString(v); s != "" {
			in.Email = s
		}
	}
	return nil
}

func inputFromUser(u store.SCIMUser) platform.SCIMUserInput {
	return platform.SCIMUserInput{UserName: u.UserName, ExternalID: u.ExternalID, Email: u.Email,
		GivenName: u.GivenName, FamilyName: u.FamilyName, DisplayName: u.DisplayName, Active: u.Active}
}

var scimFilterRe = regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9._]*)\s+eq\s+"((?:[^"\\]|\\.)*)"\s*$`)

// parseFilter supports the equality filters IdPs use to look resources up
// before creating them; allowed maps lowercase SCIM attribute -> store attr.
func parseFilter(raw string, allowed map[string]string) (store.SCIMUserFilter, bool) {
	if strings.TrimSpace(raw) == "" {
		return store.SCIMUserFilter{}, true
	}
	m := scimFilterRe.FindStringSubmatch(raw)
	if m == nil {
		return store.SCIMUserFilter{}, false
	}
	attr, ok := allowed[strings.ToLower(m[1])]
	if !ok {
		return store.SCIMUserFilter{}, false
	}
	val := strings.ReplaceAll(strings.ReplaceAll(m[2], `\"`, `"`), `\\`, `\`)
	return store.SCIMUserFilter{Attr: attr, Value: val}, true
}

// paging reads startIndex (1-based) and count.
func paging(r *http.Request) (offset, limit, start int) {
	start, _ = strconv.Atoi(r.URL.Query().Get("startIndex"))
	if start < 1 {
		start = 1
	}
	limit = scimDefaultCount
	if c := r.URL.Query().Get("count"); c != "" {
		limit, _ = strconv.Atoi(c)
	}
	if limit < 0 {
		limit = 0
	}
	if limit > scimMaxPageSize {
		limit = scimMaxPageSize
	}
	return start - 1, limit, start
}

// ---- discovery -------------------------------------------------------------------

func (s *Server) scimServiceProviderConfig(w http.ResponseWriter, r *http.Request) {
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://app.black-tulip.nl/docs/scim",
		"patch":            map[string]bool{"supported": true},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": true, "maxResults": scimMaxPageSize},
		"changePassword":   map[string]bool{"supported": false},
		"sort":             map[string]bool{"supported": false},
		"etag":             map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token", "primary": true,
			"description": "Organization provisioning token (qsc_…) from the organization's settings.",
		}},
		"meta": map[string]string{"resourceType": "ServiceProviderConfig", "location": scimBase(r) + "/ServiceProviderConfig"},
	})
}

func (s *Server) scimResourceTypes(w http.ResponseWriter, r *http.Request) {
	rt := func(name, endpoint, schema string) map[string]any {
		return map[string]any{
			"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			"id":      name, "name": name, "endpoint": endpoint, "schema": schema,
			"meta": map[string]string{"resourceType": "ResourceType", "location": scimBase(r) + "/ResourceTypes/" + name},
		}
	}
	scimJSON(w, http.StatusOK, scimList{Schemas: []string{scimListSchema}, TotalResults: 2, StartIndex: 1, ItemsPerPage: 2,
		Resources: []any{rt("User", "/Users", scimUserSchema), rt("Group", "/Groups", scimGroupSchema)}})
}

func (s *Server) scimSchemas(w http.ResponseWriter, r *http.Request) {
	attr := func(name, typ string, required bool, uniqueness string) map[string]any {
		return map[string]any{"name": name, "type": typ, "multiValued": false, "required": required,
			"caseExact": false, "mutability": "readWrite", "returned": "default", "uniqueness": uniqueness}
	}
	user := map[string]any{"id": scimUserSchema, "name": "User", "attributes": []any{
		attr("userName", "string", true, "server"), attr("externalId", "string", false, "none"),
		attr("displayName", "string", false, "none"), attr("active", "boolean", false, "none"),
		map[string]any{"name": "name", "type": "complex", "multiValued": false, "required": false, "subAttributes": []any{
			attr("givenName", "string", false, "none"), attr("familyName", "string", false, "none")}},
		map[string]any{"name": "emails", "type": "complex", "multiValued": true, "required": false, "subAttributes": []any{
			attr("value", "string", false, "none"), attr("type", "string", false, "none"), attr("primary", "boolean", false, "none")}},
	}}
	group := map[string]any{"id": scimGroupSchema, "name": "Group", "attributes": []any{
		attr("displayName", "string", true, "server"), attr("externalId", "string", false, "none"),
		map[string]any{"name": "members", "type": "complex", "multiValued": true, "required": false, "subAttributes": []any{
			attr("value", "string", false, "none"), attr("display", "string", false, "none")}},
	}}
	scimJSON(w, http.StatusOK, scimList{Schemas: []string{scimListSchema}, TotalResults: 2, StartIndex: 1, ItemsPerPage: 2, Resources: []any{user, group}})
}

// ---- users -------------------------------------------------------------------------

var userFilterAttrs = map[string]string{"username": "userName", "externalid": "externalId", "emails.value": "email", "emails": "email", "id": "id"}

func (s *Server) scimListUsers(w http.ResponseWriter, r *http.Request) {
	f, ok := parseFilter(r.URL.Query().Get("filter"), userFilterAttrs)
	if !ok {
		scimError(w, http.StatusBadRequest, "invalidFilter", `supported filters: userName, externalId, emails.value or id with "eq"`)
		return
	}
	offset, limit, start := paging(r)
	users, total, err := s.platform.SCIMListUsers(r.Context(), scimTenant(r), f, offset, limit)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	res := make([]any, 0, len(users))
	for _, u := range users {
		res = append(res, userOut(r, u))
	}
	scimJSON(w, http.StatusOK, scimList{Schemas: []string{scimListSchema}, TotalResults: total, StartIndex: start, ItemsPerPage: len(res), Resources: res})
}

func (s *Server) scimCreateUser(w http.ResponseWriter, r *http.Request) {
	var attrs map[string]any
	if !readSCIMBody(w, r, &attrs) {
		return
	}
	in := platform.SCIMUserInput{Active: true}
	if err := applyUserAttrs(&in, attrs); err != nil {
		s.scimPlatformError(w, err)
		return
	}
	u, err := s.platform.SCIMCreateUser(r.Context(), scimTenant(r), in)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimAudit(r, "scim.user_provisioned", u.ID.String(), map[string]any{"userName": u.UserName, "active": u.Active})
	w.Header().Set("Location", scimBase(r)+"/Users/"+u.ID.String())
	scimJSON(w, http.StatusCreated, userOut(r, u))
}

func (s *Server) scimGetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	u, err := s.platform.SCIMGetUser(r.Context(), scimTenant(r), id)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	scimJSON(w, http.StatusOK, userOut(r, u))
}

func (s *Server) scimPutUser(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	var attrs map[string]any
	if !readSCIMBody(w, r, &attrs) {
		return
	}
	// PUT replaces: absent attributes reset; active defaults to true.
	in := platform.SCIMUserInput{Active: true}
	if err := applyUserAttrs(&in, attrs); err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimReplaceUser(w, r, id, in)
}

func (s *Server) scimReplaceUser(w http.ResponseWriter, r *http.Request, id uuid.UUID, in platform.SCIMUserInput) {
	before, err := s.platform.SCIMGetUser(r.Context(), scimTenant(r), id)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	u, err := s.platform.SCIMReplaceUser(r.Context(), scimTenant(r), id, in)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	action := "scim.user_updated"
	switch {
	case before.Active && !u.Active:
		action = "scim.user_deprovisioned"
	case !before.Active && u.Active:
		action = "scim.user_reactivated"
	}
	s.scimAudit(r, action, u.ID.String(), map[string]any{"userName": u.UserName})
	scimJSON(w, http.StatusOK, userOut(r, u))
}

type scimPatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type scimPatch struct {
	Schemas    []string      `json:"schemas"`
	Operations []scimPatchOp `json:"Operations"`
}

func (s *Server) scimPatchUser(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	var p scimPatch
	if !readSCIMBody(w, r, &p) {
		return
	}
	cur, err := s.platform.SCIMGetUser(r.Context(), scimTenant(r), id)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	in := inputFromUser(cur)
	for _, op := range p.Operations {
		switch strings.ToLower(op.Op) {
		case "add", "replace":
			if op.Path == "" {
				m, ok := op.Value.(map[string]any)
				if !ok {
					scimError(w, http.StatusBadRequest, "invalidValue", "a path-less operation needs an object value")
					return
				}
				err = applyUserAttrs(&in, m)
			} else {
				err = applyUserAttr(&in, op.Path, op.Value)
			}
		case "remove":
			switch strings.ToLower(op.Path) {
			case "externalid":
				in.ExternalID = ""
			case "displayname":
				in.DisplayName = ""
			case "name.givenname":
				in.GivenName = ""
			case "name.familyname":
				in.FamilyName = ""
			}
		default:
			scimError(w, http.StatusBadRequest, "invalidSyntax", "unsupported op "+op.Op)
			return
		}
		if err != nil {
			s.scimPlatformError(w, err)
			return
		}
	}
	s.scimReplaceUser(w, r, id, in)
}

func (s *Server) scimDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	u, err := s.platform.SCIMGetUser(r.Context(), scimTenant(r), id)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	if err := s.platform.SCIMDeleteUser(r.Context(), scimTenant(r), id); err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimAudit(r, "scim.user_deleted", id.String(), map[string]any{"userName": u.UserName})
	w.WriteHeader(http.StatusNoContent)
}

// ---- groups --------------------------------------------------------------------------

var groupFilterAttrs = map[string]string{"displayname": "displayName", "externalid": "externalId", "id": "id"}

func (s *Server) scimListGroups(w http.ResponseWriter, r *http.Request) {
	f, ok := parseFilter(r.URL.Query().Get("filter"), groupFilterAttrs)
	if !ok {
		scimError(w, http.StatusBadRequest, "invalidFilter", `supported filters: displayName, externalId or id with "eq"`)
		return
	}
	withMembers := !strings.Contains(strings.ToLower(r.URL.Query().Get("excludedAttributes")), "members")
	offset, limit, start := paging(r)
	groups, total, err := s.platform.SCIMListGroups(r.Context(), scimTenant(r), f, offset, limit, withMembers)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	res := make([]any, 0, len(groups))
	for _, g := range groups {
		res = append(res, groupOut(r, g))
	}
	scimJSON(w, http.StatusOK, scimList{Schemas: []string{scimListSchema}, TotalResults: total, StartIndex: start, ItemsPerPage: len(res), Resources: res})
}

type scimGroupIn struct {
	DisplayName string          `json:"displayName"`
	ExternalID  string          `json:"externalId"`
	Members     []scimMemberRef `json:"members"`
}

// memberIDs parses member references; malformed ids are input errors.
func memberIDs(refs []scimMemberRef) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(refs))
	for _, m := range refs {
		id, err := uuid.Parse(m.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: unknown member %q", platform.ErrInvalidInput, m.Value)
		}
		out = append(out, id)
	}
	return out, nil
}

// memberRefsFromValue reads a PATCH value that is a list of {value} objects
// (or a single one).
func memberRefsFromValue(v any) ([]uuid.UUID, error) {
	var refs []scimMemberRef
	raw, _ := json.Marshal(v)
	if err := json.Unmarshal(raw, &refs); err != nil {
		var one scimMemberRef
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("%w: members value must be a list of {value}", platform.ErrInvalidInput)
		}
		refs = []scimMemberRef{one}
	}
	return memberIDs(refs)
}

func (s *Server) scimCreateGroup(w http.ResponseWriter, r *http.Request) {
	var in scimGroupIn
	if !readSCIMBody(w, r, &in) {
		return
	}
	ids, err := memberIDs(in.Members)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	g, err := s.platform.SCIMCreateGroup(r.Context(), scimTenant(r), in.DisplayName, in.ExternalID, ids)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimAudit(r, "scim.group_created", g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(g.Members)})
	w.Header().Set("Location", scimBase(r)+"/Groups/"+g.ID.String())
	scimJSON(w, http.StatusCreated, groupOut(r, g))
}

func (s *Server) scimGetGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	g, err := s.platform.SCIMGetGroup(r.Context(), scimTenant(r), id)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	scimJSON(w, http.StatusOK, groupOut(r, g))
}

func (s *Server) scimPutGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	var in scimGroupIn
	if !readSCIMBody(w, r, &in) {
		return
	}
	ids, err := memberIDs(in.Members)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	g, err := s.platform.SCIMUpdateGroup(r.Context(), scimTenant(r), id, platform.SCIMGroupChange{
		DisplayName: &in.DisplayName, ExternalID: &in.ExternalID, SetMembers: &ids,
	})
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimAudit(r, "scim.group_updated", g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(g.Members)})
	scimJSON(w, http.StatusOK, groupOut(r, g))
}

var memberFilterPathRe = regexp.MustCompile(`(?i)^members\[\s*value\s+eq\s+"([^"]+)"\s*\]$`)

func (s *Server) scimPatchGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	var p scimPatch
	if !readSCIMBody(w, r, &p) {
		return
	}
	var ch platform.SCIMGroupChange
	for _, op := range p.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		kind := strings.ToLower(op.Op)
		var err error
		switch {
		case kind != "add" && kind != "replace" && kind != "remove":
			scimError(w, http.StatusBadRequest, "invalidSyntax", "unsupported op "+op.Op)
			return
		case path == "members" && kind == "add":
			var ids []uuid.UUID
			ids, err = memberRefsFromValue(op.Value)
			ch.Add = append(ch.Add, ids...)
		case path == "members" && kind == "replace":
			var ids []uuid.UUID
			ids, err = memberRefsFromValue(op.Value)
			ch.SetMembers = &ids
		case path == "members" && kind == "remove":
			if op.Value == nil {
				empty := []uuid.UUID{}
				ch.SetMembers = &empty // remove all members
				break
			}
			var ids []uuid.UUID
			ids, err = memberRefsFromValue(op.Value)
			ch.Remove = append(ch.Remove, ids...)
		case kind == "remove" && memberFilterPathRe.MatchString(op.Path):
			var mid uuid.UUID
			mid, err = uuid.Parse(memberFilterPathRe.FindStringSubmatch(op.Path)[1])
			if err != nil {
				err = fmt.Errorf("%w: unknown member", platform.ErrInvalidInput)
			}
			ch.Remove = append(ch.Remove, mid)
		case path == "displayname":
			name := scimString(op.Value)
			ch.DisplayName = &name
		case path == "externalid":
			ext := scimString(op.Value)
			ch.ExternalID = &ext
		case path == "" && kind != "remove":
			m, _ := op.Value.(map[string]any)
			for k, v := range m {
				switch strings.ToLower(k) {
				case "displayname":
					name := scimString(v)
					ch.DisplayName = &name
				case "externalid":
					ext := scimString(v)
					ch.ExternalID = &ext
				case "members":
					var ids []uuid.UUID
					if ids, err = memberRefsFromValue(v); err == nil {
						if kind == "replace" {
							ch.SetMembers = &ids
						} else {
							ch.Add = append(ch.Add, ids...)
						}
					}
				}
			}
		default:
			scimError(w, http.StatusBadRequest, "noTarget", "unsupported path "+op.Path)
			return
		}
		if err != nil {
			s.scimPlatformError(w, err)
			return
		}
	}
	g, err := s.platform.SCIMUpdateGroup(r.Context(), scimTenant(r), id, ch)
	if err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimAudit(r, "scim.group_updated", g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(g.Members)})
	scimJSON(w, http.StatusOK, groupOut(r, g))
}

func (s *Server) scimDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := scimID(w, r)
	if !ok {
		return
	}
	if err := s.platform.SCIMDeleteGroup(r.Context(), scimTenant(r), id); err != nil {
		s.scimPlatformError(w, err)
		return
	}
	s.scimAudit(r, "scim.group_deleted", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- token management (org admins, regular API) --------------------------------------

type scimTokenResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	Token      string     `json:"token,omitempty"` // only on creation
}

func (s *Server) handleListSCIMTokens(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	toks, err := s.platform.ListSCIMTokens(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list provisioning tokens")
		return
	}
	out := make([]scimTokenResponse, 0, len(toks))
	for _, t := range toks {
		out = append(out, scimTokenResponse{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tokens": out, "baseUrl": scimBase(r)})
}

func (s *Server) handleCreateSCIMToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req)
	slug := chi.URLParam(r, "slug")
	raw, tok, err := s.platform.CreateSCIMToken(r.Context(), actor, slug, req.Name)
	if err != nil {
		s.writePlatformError(w, err, "could not create provisioning token")
		return
	}
	s.logAudit(r, "scim.token_created", "org", slug, map[string]any{"org": slug, "tokenId": tok.ID, "name": tok.Name})
	httpx.JSON(w, http.StatusCreated, scimTokenResponse{ID: tok.ID, Name: tok.Name, CreatedAt: tok.CreatedAt, Token: raw})
}

func (s *Server) handleRevokeSCIMToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "token not found")
		return
	}
	slug := chi.URLParam(r, "slug")
	if err := s.platform.RevokeSCIMToken(r.Context(), actor, slug, id); err != nil {
		s.writePlatformError(w, err, "could not revoke provisioning token")
		return
	}
	s.logAudit(r, "scim.token_revoked", "org", slug, map[string]any{"org": slug, "tokenId": id})
	w.WriteHeader(http.StatusNoContent)
}
