package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

type scimFixture struct {
	t   *testing.T
	srv *Server
	st  *store.Store
}

func newSCIMFixture(t *testing.T) *scimFixture {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping SCIM integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	clean := func() {
		ctx := context.Background()
		_, _ = st.Pool().Exec(ctx, `DELETE FROM projects WHERE slug LIKE 'scimt-%'`)
		_, _ = st.Pool().Exec(ctx, `DELETE FROM tenants WHERE slug LIKE 'scimt-%'`)
		_, _ = st.Pool().Exec(ctx, `DELETE FROM users WHERE email LIKE '%@scimt.test'`)
	}
	clean()
	t.Cleanup(clean)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return &scimFixture{t: t, srv: New(cfg, logging.New("error", "text"), st), st: st}
}

func (f *scimFixture) user(name string) uuid.UUID {
	f.t.Helper()
	u, err := f.st.CreateUser(context.Background(), db.CreateUserParams{Username: name, Email: name + "@scimt.test", DisplayName: name, IsActive: true})
	if err != nil {
		f.t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// org creates an org owned by a fresh admin and returns a provisioning token.
func (f *scimFixture) org(slug string) (string, platform.Actor) {
	f.t.Helper()
	admin := platform.Actor{UserID: f.user(slug + "-admin")}
	if _, _, err := f.srv.platform.CreateOrganization(context.Background(), admin, slug, slug); err != nil {
		f.t.Fatalf("create org: %v", err)
	}
	tok, _, err := f.srv.platform.CreateSCIMToken(context.Background(), admin, slug, "okta")
	if err != nil {
		f.t.Fatalf("create token: %v", err)
	}
	return tok, admin
}

func (f *scimFixture) do(token, method, path, body string) (int, map[string]any) {
	f.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/scim+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusNoContent && !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/scim+json") {
		f.t.Fatalf("%s %s: content-type %q", method, path, rec.Header().Get("Content-Type"))
	}
	return rec.Code, out
}

func (f *scimFixture) isMember(slug string, userID uuid.UUID) bool {
	f.t.Helper()
	var n int
	_ = f.st.Pool().QueryRow(context.Background(), `SELECT count(*) FROM tenant_members tm JOIN tenants t ON t.id = tm.tenant_id WHERE t.slug = $1 AND tm.user_id = $2`, slug, userID).Scan(&n)
	return n == 1
}

func (f *scimFixture) role(slug string, userID uuid.UUID) string {
	f.t.Helper()
	var role string
	_ = f.st.Pool().QueryRow(context.Background(), `SELECT role FROM tenant_members tm JOIN tenants t ON t.id = tm.tenant_id WHERE t.slug = $1 AND tm.user_id = $2`, slug, userID).Scan(&role)
	return role
}

func (f *scimFixture) groupMembers(scimGroupID string) []uuid.UUID {
	f.t.Helper()
	rows, err := f.st.Pool().Query(context.Background(), `SELECT gm.user_id FROM group_members gm JOIN scim_groups sg ON sg.group_id = gm.group_id WHERE sg.id = $1 ORDER BY gm.user_id`, scimGroupID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

func (f *scimFixture) userIDOf(scimUserID string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.st.Pool().QueryRow(context.Background(), `SELECT user_id FROM scim_users WHERE id = $1`, scimUserID).Scan(&id); err != nil {
		f.t.Fatalf("scim user %s: %v", scimUserID, err)
	}
	return id
}

func TestSCIMAuthAndDiscovery(t *testing.T) {
	f := newSCIMFixture(t)
	tok, admin := f.org("scimt-auth")

	if code, _ := f.do("", "GET", "/scim/v2/Users", ""); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := f.do("qsc_wrong", "GET", "/scim/v2/Users", ""); code != 401 {
		t.Fatalf("bad token: %d", code)
	}
	code, spc := f.do(tok, "GET", "/scim/v2/ServiceProviderConfig", "")
	if code != 200 || spc["patch"].(map[string]any)["supported"] != true {
		t.Fatalf("ServiceProviderConfig: %d %v", code, spc)
	}
	if code, rt := f.do(tok, "GET", "/scim/v2/ResourceTypes", ""); code != 200 || rt["totalResults"] != float64(2) {
		t.Fatalf("ResourceTypes: %d %v", code, rt)
	}
	if code, sc := f.do(tok, "GET", "/scim/v2/Schemas", ""); code != 200 || sc["totalResults"] != float64(2) {
		t.Fatalf("Schemas: %d %v", code, sc)
	}

	// Revoked tokens stop working at once.
	toks, err := f.srv.platform.ListSCIMTokens(context.Background(), admin, "scimt-auth")
	if err != nil || len(toks) != 1 {
		t.Fatalf("list tokens: %v %v", toks, err)
	}
	if err := f.srv.platform.RevokeSCIMToken(context.Background(), admin, "scimt-auth", toks[0].ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.do(tok, "GET", "/scim/v2/Users", ""); code != 401 {
		t.Fatalf("revoked token: %d", code)
	}

	// Only org admins mint tokens.
	member := platform.Actor{UserID: f.user("scimt-auth-member")}
	if _, _, err := f.srv.platform.CreateSCIMToken(context.Background(), member, "scimt-auth", "x"); err == nil {
		t.Fatal("non-member minted a SCIM token")
	}
}

// TestSCIMOktaUserLifecycle follows Okta's provisioning calls: look up by
// userName, create, read, deactivate via PATCH, reactivate, delete.
func TestSCIMOktaUserLifecycle(t *testing.T) {
	f := newSCIMFixture(t)
	tok, _ := f.org("scimt-okta")

	code, list := f.do(tok, "GET", `/scim/v2/Users?filter=userName%20eq%20%22alice%40scimt.test%22&startIndex=1&count=100`, "")
	if code != 200 || list["totalResults"] != float64(0) {
		t.Fatalf("lookup before create: %d %v", code, list)
	}

	okta := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@scimt.test","name":{"givenName":"Alice","familyName":"Doe"},"emails":[{"primary":true,"value":"alice@scimt.test","type":"work"}],"displayName":"Alice Doe","locale":"en-US","externalId":"00u1okta","groups":[],"password":"ignored","active":true}`
	code, alice := f.do(tok, "POST", "/scim/v2/Users", okta)
	if code != 201 {
		t.Fatalf("create: %d %v", code, alice)
	}
	aliceID := alice["id"].(string)
	if alice["userName"] != "alice@scimt.test" || alice["active"] != true || alice["externalId"] != "00u1okta" {
		t.Fatalf("created = %v", alice)
	}
	if alice["name"].(map[string]any)["givenName"] != "Alice" {
		t.Fatalf("name = %v", alice["name"])
	}
	quillAlice := f.userIDOf(aliceID)
	if !f.isMember("scimt-okta", quillAlice) {
		t.Fatal("provisioned user is not an org member")
	}
	if code, _ := f.do(tok, "POST", "/scim/v2/Users", okta); code != 409 {
		t.Fatalf("duplicate create: %d", code)
	}

	code, list = f.do(tok, "GET", `/scim/v2/Users?filter=userName%20eq%20%22ALICE%40scimt.test%22`, "")
	if code != 200 || list["totalResults"] != float64(1) {
		t.Fatalf("case-insensitive lookup: %d %v", code, list)
	}
	if code, got := f.do(tok, "GET", "/scim/v2/Users/"+aliceID, ""); code != 200 || got["id"] != aliceID {
		t.Fatalf("get: %d %v", code, got)
	}
	if code, _ := f.do(tok, "GET", `/scim/v2/Users?filter=title%20eq%20%22x%22`, ""); code != 400 {
		t.Fatalf("unsupported filter: %d", code)
	}

	// Okta deactivation.
	code, got := f.do(tok, "PATCH", "/scim/v2/Users/"+aliceID, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}`)
	if code != 200 || got["active"] != false {
		t.Fatalf("deactivate: %d %v", code, got)
	}
	if f.isMember("scimt-okta", quillAlice) {
		t.Fatal("deactivated user still an org member")
	}
	// Okta profile push (PUT) reactivates and updates.
	code, got = f.do(tok, "PUT", "/scim/v2/Users/"+aliceID, strings.Replace(okta, `"Alice Doe"`, `"Alice D."`, 1))
	if code != 200 || got["active"] != true || got["displayName"] != "Alice D." {
		t.Fatalf("put: %d %v", code, got)
	}
	if !f.isMember("scimt-okta", quillAlice) {
		t.Fatal("reactivated user is not a member")
	}
	if code, _ := f.do(tok, "DELETE", "/scim/v2/Users/"+aliceID, ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := f.do(tok, "GET", "/scim/v2/Users/"+aliceID, ""); code != 404 {
		t.Fatalf("get after delete: %d", code)
	}
	if f.isMember("scimt-okta", quillAlice) {
		t.Fatal("deleted user still an org member")
	}
	if code, _ := f.do(tok, "GET", "/scim/v2/Users/not-a-uuid", ""); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
}

// TestSCIMEntraUserAndGroups follows Microsoft Entra ID: UPN userNames,
// string booleans, Replace with capital R, path-less dotted keys, groups with
// excludedAttributes=members and members[value eq] removal.
func TestSCIMEntraUserAndGroups(t *testing.T) {
	f := newSCIMFixture(t)
	tok, _ := f.org("scimt-entra")

	entra := func(upn, ext string) string {
		return fmt.Sprintf(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User","urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"],"externalId":%q,"userName":%q,"active":true,"displayName":"%s","emails":[{"primary":true,"type":"work","value":%q}],"meta":{"resourceType":"User"},"name":{"formatted":"x","familyName":"F","givenName":"G"},"title":"Engineer","urn:ietf:params:scim:schemas:extension:enterprise:2.0:User":{"department":"R&D"}}`, ext, upn, upn, upn)
	}
	code, carol := f.do(tok, "POST", "/scim/v2/Users", entra("carol@scimt.test", "e-carol"))
	if code != 201 {
		t.Fatalf("create carol: %d %v", code, carol)
	}
	code, dave := f.do(tok, "POST", "/scim/v2/Users", entra("dave@scimt.test", "e-dave"))
	if code != 201 {
		t.Fatalf("create dave: %d %v", code, dave)
	}
	carolID, daveID := carol["id"].(string), dave["id"].(string)

	if code, l := f.do(tok, "GET", `/scim/v2/Users?filter=externalId%20eq%20%22e-dave%22`, ""); code != 200 || l["totalResults"] != float64(1) {
		t.Fatalf("externalId lookup: %d %v", code, l)
	}

	// Entra attribute updates: path-less dotted keys + string booleans.
	code, got := f.do(tok, "PATCH", "/scim/v2/Users/"+carolID, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","value":{"name.givenName":"Caroline","displayName":"Caroline F"}},{"op":"Add","path":"emails[type eq \"work\"].value","value":"carol@scimt.test"},{"op":"Replace","path":"title","value":"Lead"}]}`)
	if code != 200 || got["displayName"] != "Caroline F" || got["name"].(map[string]any)["givenName"] != "Caroline" {
		t.Fatalf("entra patch: %d %v", code, got)
	}

	// Group with carol; Entra creates with no members, then adds.
	code, grp := f.do(tok, "POST", "/scim/v2/Groups", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"externalId":"g-eng","displayName":"Engineering","members":[]}`)
	if code != 201 {
		t.Fatalf("create group: %d %v", code, grp)
	}
	gid := grp["id"].(string)
	if code, _ := f.do(tok, "POST", "/scim/v2/Groups", `{"displayName":"engineering"}`); code != 409 {
		t.Fatalf("duplicate group: %d", code)
	}
	add := fmt.Sprintf(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Add","path":"members","value":[{"value":%q},{"value":%q}]}]}`, carolID, daveID)
	if code, g := f.do(tok, "PATCH", "/scim/v2/Groups/"+gid, add); code != 200 || len(g["members"].([]any)) != 2 {
		t.Fatalf("add members: %d %v", code, g)
	}
	if got := f.groupMembers(gid); len(got) != 2 {
		t.Fatalf("group_members = %v", got)
	}
	code, gl := f.do(tok, "GET", `/scim/v2/Groups?excludedAttributes=members&filter=displayName%20eq%20%22Engineering%22`, "")
	if code != 200 || gl["totalResults"] != float64(1) {
		t.Fatalf("group lookup: %d %v", code, gl)
	}
	if res := gl["Resources"].([]any)[0].(map[string]any); res["members"] != nil {
		t.Fatalf("excludedAttributes=members still returned members: %v", res)
	}

	// Entra removal syntax.
	rm := fmt.Sprintf(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Remove","path":"members[value eq \"%s\"]"}]}`, daveID)
	if code, _ := f.do(tok, "PATCH", "/scim/v2/Groups/"+gid, rm); code != 200 {
		t.Fatalf("remove member: %d", code)
	}
	if got := f.groupMembers(gid); len(got) != 1 || got[0] != f.userIDOf(carolID) {
		t.Fatalf("after removal = %v", got)
	}

	// Okta-style rename: path-less replace {id, displayName}.
	if code, g := f.do(tok, "PATCH", "/scim/v2/Groups/"+gid, fmt.Sprintf(`{"Operations":[{"op":"replace","value":{"id":%q,"displayName":"Platform"}}]}`, gid)); code != 200 || g["displayName"] != "Platform" {
		t.Fatalf("rename: %d %v", code, g)
	}
	var quillName string
	_ = f.st.Pool().QueryRow(context.Background(), `SELECT g.name FROM groups g JOIN scim_groups sg ON sg.group_id = g.id WHERE sg.id = $1`, gid).Scan(&quillName)
	if quillName != "Platform" {
		t.Fatalf("quill group name = %q", quillName)
	}

	// Deactivating carol (Entra string boolean) removes her from the group
	// too — a group grant must never outlive deprovisioning.
	if code, _ := f.do(tok, "PATCH", "/scim/v2/Users/"+carolID, `{"Operations":[{"op":"Replace","path":"active","value":"False"}]}`); code != 200 {
		t.Fatalf("entra deactivate: %d", code)
	}
	if got := f.groupMembers(gid); len(got) != 0 {
		t.Fatalf("deprovisioned user kept group membership: %v", got)
	}
	// A deactivated person can't be (re)added to a group.
	if code, g := f.do(tok, "PATCH", "/scim/v2/Groups/"+gid, add); code != 200 || len(g["members"].([]any)) != 1 {
		t.Fatalf("re-add with inactive carol: %d %v", code, g)
	}
	// Unknown / malformed member ids are rejected.
	if code, _ := f.do(tok, "PATCH", "/scim/v2/Groups/"+gid, fmt.Sprintf(`{"Operations":[{"op":"add","path":"members","value":[{"value":%q}]}]}`, uuid.NewString())); code != 400 {
		t.Fatalf("unknown member: %d", code)
	}
	// PUT replaces the member list.
	if code, g := f.do(tok, "PUT", "/scim/v2/Groups/"+gid, `{"displayName":"Platform","members":[]}`); code != 200 || g["members"] != nil {
		t.Fatalf("put group: %d %v", code, g)
	}
	if code, _ := f.do(tok, "DELETE", "/scim/v2/Groups/"+gid, ""); code != 204 {
		t.Fatalf("delete group: %d", code)
	}
	if code, _ := f.do(tok, "GET", "/scim/v2/Groups/"+gid, ""); code != 404 {
		t.Fatalf("get deleted group: %d", code)
	}
}

// TestSCIMTenantIsolation: one org's token can never read or touch another
// org's users or groups, nor smuggle them into its own groups.
func TestSCIMTenantIsolation(t *testing.T) {
	f := newSCIMFixture(t)
	tokA, _ := f.org("scimt-iso-a")
	tokB, _ := f.org("scimt-iso-b")

	_, eve := f.do(tokA, "POST", "/scim/v2/Users", `{"userName":"eve@scimt.test","emails":[{"value":"eve@scimt.test","primary":true}]}`)
	_, grpA := f.do(tokA, "POST", "/scim/v2/Groups", `{"displayName":"A team"}`)
	eveID, gA := eve["id"].(string), grpA["id"].(string)

	for _, c := range []struct{ method, path, body string }{
		{"GET", "/scim/v2/Users/" + eveID, ""},
		{"PATCH", "/scim/v2/Users/" + eveID, `{"Operations":[{"op":"replace","value":{"active":false}}]}`},
		{"PUT", "/scim/v2/Users/" + eveID, `{"userName":"eve@scimt.test","active":false}`},
		{"DELETE", "/scim/v2/Users/" + eveID, ""},
		{"GET", "/scim/v2/Groups/" + gA, ""},
		{"DELETE", "/scim/v2/Groups/" + gA, ""},
	} {
		if code, _ := f.do(tokB, c.method, c.path, c.body); code != 404 {
			t.Fatalf("org B %s %s: %d, want 404", c.method, c.path, code)
		}
	}
	if code, l := f.do(tokB, "GET", "/scim/v2/Users", ""); code != 200 || l["totalResults"] != float64(0) {
		t.Fatalf("org B lists A's users: %v", l)
	}
	if code, l := f.do(tokB, "GET", `/scim/v2/Users?filter=id%20eq%20%22`+eveID+`%22`, ""); code != 200 || l["totalResults"] != float64(0) {
		t.Fatalf("org B id filter sees A's user: %v", l)
	}
	_, grpB := f.do(tokB, "POST", "/scim/v2/Groups", `{"displayName":"B team"}`)
	if code, _ := f.do(tokB, "PATCH", "/scim/v2/Groups/"+grpB["id"].(string), fmt.Sprintf(`{"Operations":[{"op":"add","path":"members","value":[{"value":%q}]}]}`, eveID)); code != 400 {
		t.Fatalf("org B added A's user to its group: %d", code)
	}
	if code, _ := f.do(tokB, "POST", "/scim/v2/Groups", fmt.Sprintf(`{"displayName":"B2","members":[{"value":%q}]}`, eveID)); code != 400 {
		t.Fatalf("org B created a group with A's user: %d", code)
	}
	// Eve is untouched in A.
	if code, got := f.do(tokA, "GET", "/scim/v2/Users/"+eveID, ""); code != 200 || got["active"] != true {
		t.Fatalf("A's user changed: %d %v", code, got)
	}
}

// TestSCIMExistingAccountsAndLocks: provisioning matches existing accounts by
// email without rewriting them or downgrading an admin; a locked org refuses
// new members but deprovisioning still works.
func TestSCIMExistingAccountsAndLocks(t *testing.T) {
	f := newSCIMFixture(t)
	tok, admin := f.org("scimt-lock")

	// The org's own admin, provisioned by the IdP, stays admin.
	code, adm := f.do(tok, "POST", "/scim/v2/Users", `{"userName":"scimt-lock-admin@scimt.test","displayName":"IdP Name"}`)
	if code != 201 || f.userIDOf(adm["id"].(string)) != admin.UserID {
		t.Fatalf("admin provision: %d %v", code, adm)
	}
	if f.role("scimt-lock", admin.UserID) != "admin" {
		t.Fatalf("admin downgraded to %q", f.role("scimt-lock", admin.UserID))
	}
	u, _ := f.st.GetUserByID(context.Background(), admin.UserID)
	if u.DisplayName != "scimt-lock-admin" || u.Username != "scimt-lock-admin" {
		t.Fatalf("global profile rewritten: %+v", u)
	}

	// A brand-new person gets an account with a clean username.
	code, nu := f.do(tok, "POST", "/scim/v2/Users", `{"userName":"New.Person@scimt.test","name":{"givenName":"New","familyName":"Person"}}`)
	if code != 201 {
		t.Fatalf("new person: %d %v", code, nu)
	}
	created, _ := f.st.GetUserByID(context.Background(), f.userIDOf(nu["id"].(string)))
	if created.Username != "new-person" || created.Email != "new.person@scimt.test" || created.DisplayName != "New Person" {
		t.Fatalf("created account = %+v", created)
	}
	if idents, _ := f.st.ListAuthIdentitiesForUser(context.Background(), created.ID); len(idents) != 0 {
		t.Fatalf("provisioned account has identities: %v", idents)
	}

	// No usable email: rejected.
	if code, _ := f.do(tok, "POST", "/scim/v2/Users", `{"userName":"no-email"}`); code != 400 {
		t.Fatalf("no email: %d", code)
	}

	// Lock the org: no new members, but deprovisioning works.
	if _, err := f.srv.platform.SetTenantPlan(context.Background(), platform.Actor{IsAdmin: true}, "scimt-lock", platform.PlanLapsed); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.do(tok, "POST", "/scim/v2/Users", `{"userName":"late@scimt.test"}`); code != 403 {
		t.Fatalf("create on locked org: %d", code)
	}
	nuID := nu["id"].(string)
	if code, _ := f.do(tok, "PATCH", "/scim/v2/Users/"+nuID, `{"Operations":[{"op":"replace","path":"active","value":false}]}`); code != 200 {
		t.Fatalf("deprovision on locked org: %d", code)
	}
	if f.isMember("scimt-lock", created.ID) {
		t.Fatal("deprovisioned on locked org but still a member")
	}
	if code, _ := f.do(tok, "PATCH", "/scim/v2/Users/"+nuID, `{"Operations":[{"op":"replace","path":"active","value":true}]}`); code != 403 {
		t.Fatalf("reactivate on locked org: %d", code)
	}
	// Personal tenants (free plan) can never mint tokens.
	if _, _, err := f.srv.platform.CreateSCIMToken(context.Background(), admin, "scimt-lock", "again"); err == nil {
		t.Fatal("locked org minted a new token")
	}
}
