package gitengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// The write path (serverless epic, PR B3). Design principle: NO git hooks.
// Forgejo needs receive-pack hooks because its git serving happens a process
// away from its policy layer; here the smart-HTTP handler IS the integration
// point. stateless-rpc lets us parse the client's command list (old/new/ref
// pkt-lines arrive before the pack bytes), enforce policy with plain Go
// errors, run receive-pack with buffered stdout, and commit the truth —
// packs to the bucket, the Postgres referee, then the manifest — BEFORE the
// client sees success. A rejected push therefore never touches the bucket.

// DefaultMaxPushBytes caps a single push request body (256 MiB).
const DefaultMaxPushBytes = 256 << 20

// PushLeaseTTL bounds how long a crashed push can block a repo. Generous by
// design: a renewal-less holder that exceeded it almost certainly died, and
// the expiry lets the next writer steal the lease.
const PushLeaseTTL = 10 * time.Minute

// ErrLostRace is the engine's lost-race signal from the referee: another
// writer committed the same base revision first. The client message for this
// is git-native: "fetch first, then push again".
var ErrLostRace = errors.New("referee lost race: repository state changed underneath this push")

// ErrLeaseHeld means another instance is pushing to this repo right now.
var ErrLeaseHeld = errors.New("push lease held by another writer")

// ErrFreshness means the referee and the hydrated manifest disagree: another
// instance committed between hydration and this push.
var ErrFreshness = errors.New("repository state is ahead of this instance: fetch first")

// zeroID is git's null object id: an old value of zero creates a ref, a new
// value of zero deletes one.
const zeroID = "0000000000000000000000000000000000000000"

// PushCommand is one ref update from a client push (old/new zero = create /
// delete).
type PushCommand struct {
	Old string `json:"old"`
	New string `json:"new"`
	Ref string `json:"ref"`
}

// RefereeState is the referee's view of one repo at a revision.
type RefereeState struct {
	Revision     int64
	Refs         map[string]string
	StorageBytes int64
}

// Referee is the correctness authority for ref updates (Postgres row-per-repo
// CAS in production; the S3 If-Match on the manifest is never the source of
// truth). Production adapters live in internal/platform.
type Referee interface {
	Load(ctx context.Context, repoID string) (RefereeState, error)
	// Commit advances the referee from exactly the from-state to refs. A
	// from-state that no longer matches returns ErrLostRace and commits
	// nothing.
	Commit(ctx context.Context, repoID string, from RefereeState, refs map[string]string, storageBytes int64) (RefereeState, error)
}

// ReceiveGuard is the policy checkpoint between command parsing and
// receive-pack. returning an error aborts the push with 403 before any pack
// bytes are written to storage.
type ReceiveGuard interface {
	PreReceive(ctx context.Context, rcx ReceiveContext, cmds []PushCommand, actor string) error
}

// PushLeases serializes writers per repo ACROSS instances (Postgres-backed in
// production). Acquire returns the release func on success.
type PushLeases interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (release func() error, err error)
}

// ReceiveContext is the resolved, hydrated repo for one push request. ID is
// the engine-opaque repo identity the referee/leases/guard key on; the slug
// triple addresses the bucket layout.
type ReceiveContext struct {
	ID          string
	Dir         string
	TenantID    string
	ProjectSlug string
	RepoSlug    string
	// ActorID is the authenticated pusher's user id (B6); empty for the
	// anonymous placeholder period.
	ActorID string
}

// ReceiveConfig wires the write path. A nil ReceiveConfig leaves the
// SmartHTTP read-only (pushes answered with 400 on the advertisement).
type ReceiveConfig struct {
	// Engine performs the bucket writes (manifests, packs).
	Engine *Engine
	// Resolve authorizes the request, resolves the repo and returns the
	// hydrated directory. Runs AFTER the command list is parsed.
	Resolve func(ctx context.Context, owner, repo string) (ReceiveContext, error)
	// Actor extracts the pushing identity from the request (until B6 wires
	// token auth this returns a placeceholder). Nil = "anonymous".
	Actor func(r *http.Request) string
	// Referee is the ref truth (required).
	Referee Referee
	// Guard is the pre-receive policy checkpoint (optional).
	Guard ReceiveGuard
	// Leases cross-instance-serialize writers (optional but required in any
	// multi-instance deployment).
	Leases PushLeases
	// OnPushSuccess fires after finalize committed the truth (referee +
	// manifest) for successful pushes — audit/metering live here, never in
	// the engine itself.
	OnPushSuccess func(rcx ReceiveContext, actor string)
	// OnRefsUpdated fires after OnPushSuccess with the ref commands the push
	// applied (post-receive: CI triggers, SBOM). It must not block the push —
	// implementations hand off to a goroutine.
	OnRefsUpdated func(rcx ReceiveContext, actor string, cmds []PushCommand)
	// MaxPushBytes caps the request body; 0 = DefaultMaxPushBytes.
	MaxPushBytes int64
}

// push advertisement + receive routes --------------------------------------

// serveReceiveAdvertisement answers GET /info/refs?service=git-receive-pack.
// Push is protocol v0 only: GIT_PROTOCOL is never forwarded for receive, so
// git negotiates the advertisement and command list in the shape
// parsePushCommands expects.
func (h *SmartHTTP) serveReceiveAdvertisement(w http.ResponseWriter, r *http.Request, owner, repo string) {
	if h.Receive == nil {
		http.Error(w, "push is not served by this instance", http.StatusBadRequest)
		return
	}
	rcx, err := h.Receive.Resolve(r.Context(), owner, repo)
	if err != nil {
		writeGitError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache")
	writePktLine(w, "# service=git-receive-pack\n")
	fmt.Fprint(w, "0000")
	cmd := exec.CommandContext(r.Context(), "git", "receive-pack", "--advertise-refs", "--stateless-rpc", filepath.Clean(rcx.Dir))
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		slog.Error("git receive-pack advertise failed", "err", err, "stderr", stderr.String())
		panic(http.ErrAbortHandler)
	}
}

// serveReceivePack handles POST /git-receive-pack: the full B3 flow —
// parse, authorize, lease, freshness, policy, receive-pack, finalize.
func (h *SmartHTTP) serveReceivePack(w http.ResponseWriter, r *http.Request, owner, repo string) {
	cfg := h.Receive
	if cfg == nil {
		http.Error(w, "push is not served by this instance", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-git-receive-pack-request") {
		http.Error(w, "unexpected content type", http.StatusBadRequest)
		return
	}
	max := cfg.MaxPushBytes
	if max <= 0 {
		max = DefaultMaxPushBytes
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		http.Error(w, "read push body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if int64(len(body)) > max {
		http.Error(w, fmt.Sprintf("push exceeds the %d MiB limit", max>>20), http.StatusRequestEntityTooLarge)
		return
	}
	cmds, caps, err := parsePushCommands(body)
	if err != nil {
		http.Error(w, "malformed push request: "+err.Error(), http.StatusBadRequest)
		return
	}

	rcx, err := cfg.Resolve(r.Context(), owner, repo)
	if err != nil {
		writeGitError(w, err)
		return
	}
	m, etag, err := cfg.Engine.LoadManifest(r.Context(), rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		http.Error(w, "repository state unreadable", http.StatusInternalServerError)
		return
	}

	// Cross-instance single writer: the lease slots between resolution and
	// the freshness check so the whole verify+commit section is exclusive.
	if cfg.Leases != nil {
		release, err := cfg.Leases.Acquire(r.Context(), rcx.ID, PushLeaseTTL)
		if err != nil {
			if errors.Is(err, ErrLeaseHeld) {
				http.Error(w, "another push is in flight; retry shortly", http.StatusLocked)
				return
			}
			http.Error(w, "acquire push lease: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = release() }()
	}

	st, err := cfg.Referee.Load(r.Context(), rcx.ID)
	if err != nil {
		http.Error(w, "repository state unreadable", http.StatusInternalServerError)
		return
	}
	if !refsEqual(st.Refs, manifestRefs(m)) {
		http.Error(w, ErrFreshness.Error(), http.StatusConflict)
		return
	}

	actor := "anonymous"
	if cfg.Actor != nil {
		if a := cfg.Actor(r); a != "" {
			actor = a
		}
	}
	if cfg.Guard != nil {
		if err := cfg.Guard.PreReceive(r.Context(), rcx, cmds, actor); err != nil {
			// Reject in-band as a receive-pack status report: git clients
			// render "! [remote rejected] <ref> (<reason>)" for exactly this
			// shape — the same UX a hook decline produces. HTTP-level error
			// codes would hide the reason behind "the requested URL
			// returned error". report-status is only sent when negotiated.
			if strings.Contains(caps, "report-status") {
				var report bytes.Buffer
				writePktLine(&report, "unpack ok\n")
				for _, c := range cmds {
					writePktLine(&report, fmt.Sprintf("ng %s %s\n", c.Ref, err.Error()))
				}
				fmt.Fprint(&report, "0000")
				writeReport(w, report.Bytes(), caps)
			} else {
				fmt.Fprint(w, "0000")
			}
			return
		}
	}

	// receive.unpackLimit=1: every incoming pack stays a packfile. The
	// bucket is the repository — loose objects would never be persisted, so
	// the receive-pack default (explode small pushes into loose objects
	// below the 100-object limit) would silently lose them. gc.auto=0: no
	// background gc on a disposable cache (repairs/maintenance are B7 jobs).
	if err := prepareReceiveRepo(rcx.Dir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// run receive-pack with buffered stdout: the client learns the outcome
	// only after the truth committed (a failure below never follows a
	// protocol-level success).
	var out bytes.Buffer
	if err := runReceivePack(r, rcx.Dir, body, &out); err != nil {
		slog.Error("git receive-pack failed", "err", err)
		http.Error(w, "receive-pack failed", http.StatusInternalServerError)
		return
	}

	if err := h.finalizePush(r.Context(), cfg, rcx, m, etag, st, cmds, actor); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrLostRace), errors.Is(err, ErrFreshness):
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	if cfg.OnPushSuccess != nil {
		cfg.OnPushSuccess(rcx, actor)
	}
	if cfg.OnRefsUpdated != nil {
		cfg.OnRefsUpdated(rcx, actor, cmds)
	}

	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(out.Bytes())
}

// finalizePush re-raises the hydrated repo into the bucket after a push.
func (h *SmartHTTP) finalizePush(ctx context.Context, cfg *ReceiveConfig, rcx ReceiveContext, m *Manifest, etag string, st RefereeState, cmds []PushCommand, actor string) error {
	pushed := cmds[0].New
	for _, c := range cmds {
		if c.New != "" && c.New != zeroID {
			pushed = c.New
			break
		}
	}
	return SyncRepoToBucket(ctx, cfg.Engine, cfg.Referee, rcx, m, etag, st, pushed, actor)
}

// SyncRepoToBucket commits a hydrated repo's CURRENT on-disk state as the new
// truth: refs become the manifest, packs not yet in the bucket are uploaded,
// the referee CAS advances FIRST (Postgres is the truth), and the manifest
// write follows through If-Match with bounded retries. Shared by the push
// handler (B3) and merges/rebase-created commits (B5): the caller is
// responsible for holding the write lease and for the state being fresh.
// pushedSha lands in push_history's tail entry.
//
// Cache hygiene: receive-pack (and merges) write LOOSE refs, which shadow the
// packed-refs view Hydrate materializes from the manifest. A loose ref that
// outlives its push goes stale the moment another instance moves that ref —
// and would be snapshotted as "truth" on this instance's next push. So once
// the referee committed, loose refs are folded into packed-refs; if nothing
// committed (lost race, error), they are dropped so the next hydration
// re-materializes the referee's view.
func SyncRepoToBucket(ctx context.Context, eng *Engine, referee Referee, rcx ReceiveContext, m *Manifest, etag string, st RefereeState, pushedSha, actor string) (err error) {
	committed := false
	defer func() {
		if committed {
			packLooseRefs(rcx.Dir)
		} else {
			dropLooseRefs(rcx.Dir)
		}
	}()
	newRefs, err := RefSnapshot(rcx.Dir)
	if err != nil {
		return err
	}
	newMap := map[string]string{}
	for _, r := range newRefs {
		newMap[r.Name] = r.Target
	}
	if refsEqual(newMap, st.Refs) {
		// nothing changed on disk (e.g. receive-pack rejected in-band): the
		// on-disk refs ARE the committed truth, keep them.
		committed = true
		return nil
	}

	known := map[string]bool{}
	for _, p := range m.Packs {
		known[strings.TrimSuffix(filepath.Base(p.Object), ".pack")] = true
	}
	packDir := filepath.Join(rcx.Dir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return fmt.Errorf("read pack dir: %w", err)
	}
	packs := make([]ManifestPack, len(m.Packs), len(m.Packs)+4)
	copy(packs, m.Packs)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".pack") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".pack")
		if known[base] {
			continue
		}
		p, err := eng.UploadPack(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, filepath.Join(packDir, base+".pack"), filepath.Join(packDir, base+".idx"))
		if err != nil {
			return err
		}
		packs = append(packs, p)
	}

	var storage int64
	for _, p := range packs {
		storage += p.Bytes + p.IdxBytes
	}
	m2 := *m
	m2.Refs = newRefs
	m2.Packs = packs
	m2.StorageBytes = storage
	m2.RecordPush(actor, rcx.TenantID, time.Now().UTC(), pushedSha)

	if _, err := referee.Commit(ctx, rcx.ID, st, newMap, storage); err != nil {
		return err
	}
	committed = true

	// The referee committed; the manifest write is a repairable view update.
	// Retry against a fresh etag (a concurrent hydration does not write
	// manifests, but bucket read-after-write can serve a stale etag); a final
	// unconditional write is safe because the lease makes this the only
	// manifest writer. If even that fails the push is STILL durable — the
	// referee holds the truth and B7 reconciliation re-materializes views.
	var werr error
	for i := 0; i < 3; i++ {
		if _, werr = eng.WriteManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, &m2, etag); werr == nil {
			return nil
		}
		if !errors.Is(werr, objectstore.ErrPreconditionFailed) {
			break
		}
		if i == 0 {
			if _, fresh, rerr := eng.LoadManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug); rerr == nil {
				etag = fresh
			}
		} else if i > 0 {
			etag = "" // lease-held writer: unconditional on the last attempt
		}
	}
	slog.Warn("manifest write did not land after referee commit; hydration will serve the committed referee state on next sync", "err", werr)
	return nil
}

// prepareReceiveRepo stamps the per-repo config a receive-pack run needs in a
// disposable cache (idempotent; runs before every push).
func prepareReceiveRepo(dir string) error {
	for _, kv := range [][2]string{{"receive.unpackLimit", "1"}, {"gc.auto", "0"}} {
		cmd := exec.Command("git", "config", kv[0], kv[1])
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git config %s: %w: %s", kv[0], err, out)
		}
	}
	return nil
}

// writeReport sends a receive-pack status report, honoring a side-band
// negotiation: each chunk rides band 1, packet-framed, exactly like
// receive-pack's own multiplexed output.
func writeReport(w io.Writer, report []byte, caps string) {
	if !strings.Contains(caps, "side-band") {
		_, _ = w.Write(report)
		return
	}
	const chunk = 65515 // band 1 payload max (64k sideband minus band byte)
	for len(report) > 0 {
		n := len(report)
		if n > chunk {
			n = chunk
		}
		frame := append([]byte{1}, report[:n]...)
		fmt.Fprintf(w, "%04x", len(frame)+4)
		_, _ = w.Write(frame)
		report = report[n:]
	}
	fmt.Fprint(w, "0000")
}

// runReceivePack executes `git receive-pack --stateless-rpc dir` with the
// original request body (commands + pack) on stdin and the status report
// buffered on stdout.
func runReceivePack(r *http.Request, dir string, body []byte, out *bytes.Buffer) error {
	cmd := exec.CommandContext(r.Context(), "git", "receive-pack", "--stateless-rpc", filepath.Clean(dir))
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout = out
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git receive-pack: %w: %s", err, stderr.String())
	}
	return nil
}

// RefSnapshot runs for-each-ref in a repo dir (bare or hydrated), capturing
// targets and peeled tag commits — the manifest refs view of any repo dir.
// Exported for the D1 migration tool, which snapshots a mirror clone.
func RefSnapshot(dir string) ([]ManifestRef, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname) %(objectname) %(*objectname)")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("for-each-ref: %w", err)
	}
	var refs []ManifestRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		ref := ManifestRef{Name: parts[0], Target: parts[1]}
		if len(parts) > 2 && parts[2] != "" {
			ref.Peeled = parts[2]
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// refsEqual compares two ref maps by value.
func refsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// manifestRefs flattens a manifest's refs into the referee-shaped map.
func manifestRefs(m *Manifest) map[string]string {
	refs := make(map[string]string, len(m.Refs))
	for _, r := range m.Refs {
		refs[r.Name] = r.Target
	}
	return refs
}

// parsePushCommands reads the v0 receive-pack request preamble: pkt-lined
// "<old> <new> <ref>[\0<caps>]" commands until the flush that precedes the
// pack payload, plus the negotiated capability list. Push certificates are
// refused (plain commands only).
func parsePushCommands(body []byte) ([]PushCommand, string, error) {
	var cmds []PushCommand
	caps := ""
	buf := bytes.NewReader(body)
	for {
		if buf.Len() < 4 {
			return nil, caps, errors.New("truncated pkt-line stream")
		}
		var hdr [4]byte
		if _, err := io.ReadFull(buf, hdr[:]); err != nil {
			return nil, caps, err
		}
		n, err := parsePktLen(string(hdr[:]))
		if err != nil {
			return nil, caps, err
		}
		if n == 0 {
			break // flush-pkt: commands end, pack payload follows
		}
		if n < 4 || n > 65536 {
			return nil, caps, fmt.Errorf("invalid pkt-line length %d", n)
		}
		payload := make([]byte, n-4)
		if _, err := io.ReadFull(buf, payload); err != nil {
			return nil, caps, fmt.Errorf("truncated pkt-line: %w", err)
		}
		line := string(payload)
		if i := strings.IndexByte(line, 0); i >= 0 {
			caps = line[i+1:] // first command carries the capability list
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "push-cert") {
			return nil, caps, errors.New("signed push certificates are not supported")
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !isHexSha(fields[0]) || !isHexSha(fields[1]) || !strings.HasPrefix(fields[2], "refs/") {
			return nil, caps, fmt.Errorf("malformed ref update %q", line)
		}
		cmds = append(cmds, PushCommand{Old: fields[0], New: fields[1], Ref: fields[2]})
	}
	if len(cmds) == 0 {
		return nil, caps, errors.New("push carries no ref updates")
	}
	return cmds, caps, nil
}

// parsePktLen decodes a 4-hex pkt-line length; 0000 is the flush (0).
func parsePktLen(hdr string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(hdr, "%04x", &n); err != nil {
		return 0, fmt.Errorf("invalid pkt-line header %q", hdr)
	}
	return n, nil
}

func isHexSha(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// packLooseRefs folds loose refs into packed-refs (best effort: a failure only
// costs the next hydration a rewrite).
func packLooseRefs(dir string) {
	cmd := exec.Command("git", "pack-refs", "--all", "--prune")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("pack-refs after push failed", "dir", dir, "err", err, "out", strings.TrimSpace(string(out)))
	}
}

// dropLooseRefs deletes every loose ref (an uncommitted push's leftovers);
// packed-refs — the referee's materialized view — becomes authoritative again.
func dropLooseRefs(dir string) {
	refs := filepath.Join(dir, "refs")
	if err := os.RemoveAll(refs); err != nil {
		slog.Warn("drop loose refs failed", "dir", dir, "err", err)
		return
	}
	for _, sub := range []string{"heads", "tags"} {
		_ = os.MkdirAll(filepath.Join(refs, sub), 0o755)
	}
}
