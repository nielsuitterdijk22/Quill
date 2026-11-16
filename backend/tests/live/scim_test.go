package live

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSCIMProvisioning drives the deployed SCIM endpoint the way an identity
// provider does: an org admin mints a token, the IdP provisions a person (who
// joins the org), pushes a group, deactivates the person (who leaves the org
// and the group), and the revoked token stops working. The provisioned email
// is fixed so repeated runs reuse one account instead of accumulating users.
func TestSCIMProvisioning(t *testing.T) {
	requireLive(t)
	token := login(t)
	org := fmt.Sprintf("e2e-scim-%d", time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, token, http.MethodPost, "/orgs", map[string]any{"slug": org, "name": "E2E SCIM"}); st != 200 && st != 201 {
		t.Fatalf("create org: %d %s", st, b)
	}
	st, b := apiCall(t, token, http.MethodPost, "/orgs/"+org+"/scim/tokens", map[string]any{"name": "live"})
	if st != 201 {
		t.Fatalf("create scim token: %d %s", st, b)
	}
	created := jbody(t, b)
	scimTok, _ := created["token"].(string)
	tokID, _ := created["id"].(string)
	if !strings.HasPrefix(scimTok, "qsc_") {
		t.Fatal("scim token missing")
	}
	st, b = apiCall(t, token, http.MethodGet, "/orgs/"+org+"/scim/tokens", nil)
	base, _ := jbody(t, b)["baseUrl"].(string)
	if st != 200 || !strings.HasSuffix(base, "/scim/v2") {
		t.Fatalf("list tokens: %d %s", st, b)
	}
	base = apiURL + "/scim/v2" // the API under test, whatever host it is reached on

	scim := func(method, path, body string) (int, map[string]any) {
		var rdr io.Reader
		if body != "" {
			rdr = bytes.NewReader([]byte(body))
		}
		r, _ := http.NewRequest(method, base+path, rdr)
		r.Header.Set("Authorization", "Bearer "+scimTok)
		r.Header.Set("Content-Type", "application/scim+json")
		resp, err := client(t).Do(r)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	members := func() string {
		_, b := apiCall(t, token, http.MethodGet, "/orgs/"+org+"/members", nil)
		return string(b)
	}

	const email = "e2e-scim-provisioned@black-tulip.nl"
	if code, spc := scim("GET", "/ServiceProviderConfig", ""); code != 200 || spc["patch"] == nil {
		t.Fatalf("ServiceProviderConfig: %d %v", code, spc)
	}
	code, user := scim("POST", "/Users", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"`+email+`","name":{"givenName":"Scim","familyName":"Provisioned"},"emails":[{"value":"`+email+`","primary":true}],"active":true}`)
	if code != 201 {
		t.Fatalf("provision user: %d %v", code, user)
	}
	uid, _ := user["id"].(string)
	if !strings.Contains(members(), email) {
		t.Fatalf("provisioned person not in org members: %s", members())
	}
	code, grp := scim("POST", "/Groups", `{"displayName":"E2E Engineering","members":[{"value":"`+uid+`"}]}`)
	if code != 201 || len(grp["members"].([]any)) != 1 {
		t.Fatalf("create group: %d %v", code, grp)
	}
	_, b = apiCall(t, token, http.MethodGet, "/orgs/"+org+"/groups", nil)
	if !strings.Contains(string(b), "E2E Engineering") {
		t.Fatalf("SCIM group not visible in org groups: %s", b)
	}

	if code, u := scim("PATCH", "/Users/"+uid, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","path":"active","value":"False"}]}`); code != 200 || u["active"] != false {
		t.Fatalf("deactivate: %d %v", code, u)
	}
	if strings.Contains(members(), email) {
		t.Fatalf("deactivated person still in org members: %s", members())
	}
	if code, g := scim("GET", "/Groups/"+grp["id"].(string), ""); code != 200 || g["members"] != nil {
		t.Fatalf("deactivated person still in group: %d %v", code, g)
	}
	if code, _ := scim("DELETE", "/Users/"+uid, ""); code != 204 {
		t.Fatalf("delete user: %d", code)
	}

	if st, b := apiCall(t, token, http.MethodDelete, "/orgs/"+org+"/scim/tokens/"+tokID, nil); st != 204 {
		t.Fatalf("revoke: %d %s", st, b)
	}
	if code, _ := scim("GET", "/Users", ""); code != 401 {
		t.Fatalf("revoked token still works: %d", code)
	}
}
