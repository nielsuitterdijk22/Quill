package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CI run tokens (D2 cutover). A pipeline job needs to clone its own repo and
// fetch cross-repo templates (`uses: project/repo/...@ref`) from the engine.
// Instead of handing user workflow code an admin credential (what the Forgejo
// path did), each run gets a stateless token:
//
//	qgr_<base64url(tenantID "|" expiryUnix)>.<hex HMAC-SHA256>
//
// It grants READ access to repositories of the run's tenant only, never push,
// and expires (RunTokenTTL). Stateless on purpose: no table, no cleanup, and
// verification needs nothing but the secret — the api stays cold-start cheap.

const runTokenPrefix = "qgr_"

// RunTokenTTL bounds a run token's life: the longest pipeline timeout plus
// slack for queue wait. A leaked token dies on its own.
const RunTokenTTL = 4 * time.Hour

// MintRunToken issues a read-only token for repos in tenantID.
func (s *Service) MintRunToken(tenantID uuid.UUID, ttl time.Duration) (string, error) {
	if s.gitRunTokenSecret == "" {
		return "", fmt.Errorf("%w: git run tokens not configured", ErrUnavailable)
	}
	payload := tenantID.String() + "|" + strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return runTokenPrefix + enc + "." + s.runTokenMAC(enc), nil
}

func (s *Service) runTokenMAC(enc string) string {
	mac := hmac.New(sha256.New, []byte("quill-git-run-token|"+s.gitRunTokenSecret))
	mac.Write([]byte(enc))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyRunToken returns the tenant a valid, unexpired token is scoped to.
func (s *Service) verifyRunToken(tok string) (uuid.UUID, bool) {
	if s.gitRunTokenSecret == "" || !strings.HasPrefix(tok, runTokenPrefix) {
		return uuid.Nil, false
	}
	enc, sig, ok := strings.Cut(strings.TrimPrefix(tok, runTokenPrefix), ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.runTokenMAC(enc))) {
		return uuid.Nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return uuid.Nil, false
	}
	tenant, exp, ok := strings.Cut(string(raw), "|")
	if !ok {
		return uuid.Nil, false
	}
	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expUnix {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(tenant)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
