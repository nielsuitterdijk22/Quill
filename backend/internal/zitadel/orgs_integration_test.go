package zitadel

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// TestCreateAndListOrgLive exercises the Management client against a real Zitadel
// instance. Gated on QUILL_TEST_ZITADEL_ISSUER + QUILL_TEST_ZITADEL_TOKEN (e.g.
// from deploy/spike-zitadel: issuer http://localhost:8081 and the PAT in
// out/quill-api.pat) so CI without an instance stays green.
func TestCreateAndListOrgLive(t *testing.T) {
	issuer := os.Getenv("QUILL_TEST_ZITADEL_ISSUER")
	token := os.Getenv("QUILL_TEST_ZITADEL_TOKEN")
	if issuer == "" || token == "" {
		t.Skip("QUILL_TEST_ZITADEL_ISSUER/TOKEN not set; skipping live Zitadel management test")
	}
	c := New(config.ZitadelConfig{Issuer: issuer, ManagementToken: token})
	if !c.Enabled() {
		t.Fatal("client should be enabled")
	}
	ctx := context.Background()

	name := "quill-it-" + time.Now().Format("150405.000")
	orgID, err := c.CreateOrg(ctx, name, "")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if orgID == "" {
		t.Fatal("CreateOrg returned empty org id")
	}
	t.Logf("created org %s (%s)", name, orgID)

	members, err := c.ListOrgMembers(ctx, orgID)
	if err != nil {
		t.Fatalf("ListOrgMembers: %v", err)
	}
	t.Logf("org %s has %d member(s)", orgID, len(members))
}
