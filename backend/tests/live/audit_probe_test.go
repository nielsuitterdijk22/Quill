package live

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAuditLogAdmin(t *testing.T) {
	requireLive(t)
	token := login(t)
	st, b := apiCall(t, token, http.MethodGet, "/admin/audit-log?limit=5", nil)
	fmt.Printf("AUDIT as e2e user: %d %.150s\n", st, b)
}
