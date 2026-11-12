package gitengine

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// zeroID is defined in receive.go (production code shares it).

// memReferee is the in-memory referee used by engine tests. It mirrors the
// Postgres referee's CAS semantics: Commit only lands from the exact
// from-state, otherwise ErrLostRace; revisions advance by exactly one.
type memReferee struct {
	mu      sync.Mutex
	rev     int64
	refs    map[string]string
	storage int64
}

func newMemReferee(rev int64, refs map[string]string) *memReferee {
	if refs == nil {
		refs = map[string]string{}
	}
	return &memReferee{rev: rev, refs: refs}
}

func (r *memReferee) Load(ctx context.Context, repoID string) (RefereeState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	refs := make(map[string]string, len(r.refs))
	for k, v := range r.refs {
		refs[k] = v
	}
	return RefereeState{Revision: r.rev, Refs: refs, StorageBytes: r.storage}, nil
}

func (r *memReferee) Commit(ctx context.Context, repoID string, from RefereeState, refs map[string]string, storageBytes int64) (RefereeState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rev != from.Revision || !refsEqual(r.refs, from.Refs) {
		return RefereeState{}, ErrLostRace
	}
	r.rev++
	r.refs = refs
	r.storage = storageBytes
	return RefereeState{Revision: r.rev, Refs: refs, StorageBytes: r.storage}, nil
}

// refsEqual lives in receive.go (shared with the handler).

// memLeases is the in-process lease registry for tests.
type memLeases struct {
	mu     sync.Mutex
	held   map[string]bool
	called map[string]int
}

func newMemLeases() *memLeases {
	return &memLeases{held: map[string]bool{}, called: map[string]int{}}
}

func (l *memLeases) Acquire(ctx context.Context, key string, ttl time.Duration) (func() error, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		l.called[key]++
		return nil, ErrLeaseHeld
	}
	l.held[key] = true
	l.called[key]++
	return func() error {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.held, key)
		return nil
	}, nil
}

// recordingGuard records what the handler saw and consults a predicate.
type recordingGuard struct {
	mu     sync.Mutex
	cmds   []PushCommand
	actor  string
	reject func(cmd PushCommand, actor string) error
}

func (g *recordingGuard) PreReceive(ctx context.Context, rcx ReceiveContext, cmds []PushCommand, actor string) error {
	g.mu.Lock()
	g.cmds = append(g.cmds, cmds...)
	g.actor = actor
	pred := g.reject
	g.mu.Unlock()
	if pred == nil {
		return nil
	}
	for _, c := range cmds {
		if err := pred(c, actor); err != nil {
			return err
		}
	}
	return nil
}

// newReceiveFixture wires a SmartHTTP with the receive path enabled over a
// fresh MemStore, returning everything the assertions need.
type receiveFixture struct {
	store *objectstore.MemStore
	// actually the interface ObjectStore; tests only need Get/Keys
	engine  *Engine
	cache   *RepoCache
	referee *memReferee
	leases  *memLeases
	guard   *recordingGuard
	server  *httptest.Server
}

func newReceiveFixture(t *testing.T, store *objectstore.MemStore, referee *memReferee, guard *recordingGuard, leases *memLeases) *receiveFixture {
	t.Helper()
	if store == nil {
		store = objectstore.NewMemStore()
	}
	mem := store
	cache := &RepoCache{Root: t.TempDir(), Engine: New(mem), Packs: NewBucketPackSource(mem)}
	cfg := &ReceiveConfig{
		Engine:  cache.Engine,
		Referee: referee,
		Actor:   func(*http.Request) string { return "alice" },
		Resolve: func(ctx context.Context, owner, repo string) (ReceiveContext, error) {
			dir, err := cache.Hydrate(ctx, "t1", owner, repo)
			if err != nil {
				return ReceiveContext{}, err
			}
			return ReceiveContext{
				ID:  "t1/" + owner + "/" + repo,
				Dir: dir, TenantID: "t1", ProjectSlug: owner, RepoSlug: repo,
			}, nil
		},
	}
	if guard != nil {
		cfg.Guard = guard
	}
	if leases != nil {
		cfg.Leases = leases
	}
	h := &SmartHTTP{
		Hydrate: func(ctx context.Context, owner, repo string) (string, error) {
			return cache.Hydrate(ctx, "t1", owner, repo)
		},
		Receive: cfg,
	}
	srv := httptest.NewServer(http.StripPrefix("/git", http.Handler(h)))
	t.Cleanup(srv.Close)
	return &receiveFixture{
		store: mem, engine: cache.Engine, cache: cache,
		referee: referee, leases: leases, guard: guard, server: srv,
	}
}

// gitPush runs `git push` in dir with the given args, returning combined
// output + error (a rejected push is the point of several tests, so errors
// are for the caller to assert).
func gitPush(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"push"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
		"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestPushOverSmartHTTP is the write-path round trip: clone an empty repo,
// commit, push, and prove the truth moved — manifest refs, packs in the
// bucket, push history (actor + tenant), storage metering, referee revision.
// A fresh clone from the same URL must reproduce the pushed content.
func TestPushOverSmartHTTP(t *testing.T) {
	gitAvailable(t)
	referee := newMemReferee(1, nil)
	f := newReceiveFixture(t, nil, referee, nil, nil)
	url := f.server.URL + "/git/acme/widgets"
	ctx := context.Background()

	// Provision the empty repo: manifest + referee already at rev 1.
	if err := f.engine.ProvisionRepo(ctx, "t1", "acme", "widgets", "main"); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Clone the empty repo, commit, tag, push (branch + annotated tag).
	work := t.TempDir()
	clone := func(dst string) {
		t.Helper()
		cmd := exec.Command("git", "clone", "-q", url, dst)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
			"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clone: %v\n%s", err, out)
		}
	}
	clone(work)
	writeFixtureCommit(t, work, "first push", "hello.txt", "hello\n")
	gitCmd(t, work, "commit", "-qm", "the pushed commit")
	gitCmd(t, work, "tag", "-a", "v1.0", "-m", "release")
	pushed := strings.TrimSpace(gitCmd(t, work, "rev-parse", "HEAD"))
	if out, err := gitPush(t, work, "origin", "main", "--tags"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}

	// Manifest: refs recorded (main + tag with peeled target), pack pair with
	// etags, push history with actor + tenant, storage metering = pack+idx.
	m, etag, err := f.engine.LoadManifest(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	main := m.Ref("refs/heads/main")
	if main == nil || main.Target != pushed {
		t.Fatalf("manifest main = %+v, want target %s", main, pushed)
	}
	tag := m.Ref("refs/tags/v1.0")
	if tag == nil || tag.Target == "" || tag.Peeled == "" {
		t.Fatalf("manifest tag must keep target + peeled: %+v", tag)
	}
	if tag.Peeled != pushed {
		t.Fatalf("tag peeled = %s, want %s", tag.Peeled, pushed)
	}
	if len(m.Packs) != 1 {
		t.Fatalf("manifest packs = %+v, want exactly one", m.Packs)
	}
	p := m.Packs[0]
	if p.ETag == "" || p.IdxObject == "" || p.IdxETag == "" {
		t.Fatalf("pack record incomplete: %+v", p)
	}
	if p.Bytes <= 0 || p.IdxBytes <= 0 {
		t.Fatalf("pack sizes not recorded: %+v", p)
	}
	if m.StorageBytes != p.Bytes+p.IdxBytes {
		t.Fatalf("storage_bytes = %d, want %d", m.StorageBytes, p.Bytes+p.IdxBytes)
	}
	if len(m.PushHistory) != 1 {
		t.Fatalf("push history = %+v", m.PushHistory)
	}
	ph := m.PushHistory[0]
	if ph.PushedBy != "alice" || ph.TenantID != "t1" || ph.PushedSha != pushed {
		t.Fatalf("push history entry = %+v", ph)
	}
	if etag == "" {
		t.Fatal("manifest etag missing")
	}

	// The pack + idx really are in the bucket under the repo prefix.
	prefix := RepoPrefix("t1", "acme", "widgets")
	if _, _, err := f.store.Get(ctx, prefix+p.Object); err != nil {
		t.Fatalf("pack in bucket: %v", err)
	}
	if _, _, err := f.store.Get(ctx, prefix+p.IdxObject); err != nil {
		t.Fatalf("idx in bucket: %v", err)
	}

	// Referee advanced exactly one revision and agrees with the manifest.
	st, err := referee.Load(ctx, "t1/acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if st.Revision != 2 {
		t.Fatalf("referee revision = %d, want 2", st.Revision)
	}
	if st.Refs["refs/heads/main"] != pushed || st.Refs["refs/tags/v1.0"] != tag.Target {
		t.Fatalf("referee refs = %v", st.Refs)
	}
	if st.StorageBytes != m.StorageBytes {
		t.Fatalf("referee storage = %d, manifest %d", st.StorageBytes, m.StorageBytes)
	}

	// Round trip: a fresh clone reproduces the pushed content and the tag.
	second := t.TempDir()
	clone(second)
	got, err := os.ReadFile(filepath.Join(second, "hello.txt"))
	if err != nil || string(got) != "hello\n" {
		t.Fatalf("round-tripped hello.txt = %q %v", got, err)
	}
	if out := gitCmd(t, second, "describe", "--tags"); strings.TrimSpace(out) != "v1.0" {
		t.Fatalf("round-tripped tag = %q", out)
	}
	if out := gitCmd(t, second, "rev-parse", "HEAD"); strings.TrimSpace(out) != pushed {
		t.Fatalf("round-tripped head = %q", out)
	}

	// A second push (new commit on top) advances revision again and appends
	// the new pack next to the old one.
	writeFixtureCommit(t, work, "second commit", "second.txt", "second\n")
	gitCmd(t, work, "commit", "-qm", "second commit")
	pushed2 := strings.TrimSpace(gitCmd(t, work, "rev-parse", "HEAD"))
	if out, err := gitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("second push: %v\n%s", err, out)
	}
	m2, _, err := f.engine.LoadManifest(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Packs) < 2 {
		t.Fatalf("second push must keep the old pack and add a new one: %+v", m2.Packs)
	}
	if m2.Ref("refs/heads/main").Target != pushed2 {
		t.Fatalf("after second push main = %+v", m2.Ref("refs/heads/main"))
	}
	st2, _ := referee.Load(ctx, "t1/acme/widgets")
	if st2.Revision != 3 {
		t.Fatalf("referee revision after second push = %d, want 3", st2.Revision)
	}
	var sum int64
	for _, pp := range m2.Packs {
		sum += pp.Bytes + pp.IdxBytes
	}
	if m2.StorageBytes != sum {
		t.Fatalf("storage metering = %d, want %d", m2.StorageBytes, sum)
	}
}

// writeFixtureCommit stages one file and requires the caller to commit.
func writeFixtureCommit(t *testing.T, dir, name, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "-A")
}

// TestPushForceRejectedByGuard proves pre-receive enforcement happens in the
// HTTP handler: a guard that rejects the force update returns before
// receive-pack ever runs — no referee change, no pack upload.
func TestPushForceRejectedByGuard(t *testing.T) {
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	base := map[string]string{}
	for _, r := range m.Refs {
		base[r.Name] = r.Target
	}
	referee := newMemReferee(1, base)
	guard := &recordingGuard{reject: func(cmd PushCommand, actor string) error {
		if cmd.Ref == "refs/heads/main" && cmd.Old != zeroID && cmd.New != zeroID {
			return fmt.Errorf("force push to main rejected by policy")
		}
		return nil
	}}
	f := newReceiveFixture(t, mem, referee, guard, nil)
	url := f.server.URL + "/git/acme/widgets"
	ctx := context.Background()

	// Clone, rewrite history on main, force push.
	work := t.TempDir()
	clone := exec.Command("git", "clone", "-q", url, work)
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	gitCmd(t, work, "commit", "-q", "--amend", "-m", "rewritten")
	out, err := gitPush(t, work, "-f", "origin", "main")
	if err == nil {
		t.Fatalf("force push must be rejected, got success:\n%s", out)
	}
	// The rejection is an in-band receive-pack report: the client renders it
	// as a remote rejection per ref, with the guard's reason attached.
	if !strings.Contains(out, "remote rejected") {
		t.Fatalf("force push must be a remote rejection, got:\n%s", out)
	}
	if !strings.Contains(out, "policy") || !strings.Contains(out, "main -> main") {
		t.Fatalf("guard's message and ref must reach the client, got:\n%s", out)
	}

	// The guard saw the command list with old/new/ref intact.
	guard.mu.Lock()
	if len(guard.cmds) != 1 || guard.cmds[0].Ref != "refs/heads/main" ||
		guard.cmds[0].Old == zeroID || guard.cmds[0].New == zeroID ||
		guard.cmds[0].Old == guard.cmds[0].New {
		guard.mu.Unlock()
		t.Fatalf("guard saw cmds = %+v", guard.cmds)
	}
	if guard.actor != "alice" {
		guard.mu.Unlock()
		t.Fatalf("guard actor = %q", guard.actor)
	}
	guard.mu.Unlock()

	// Nothing moved: referee revision, manifest etag and pack set unchanged.
	st, _ := referee.Load(ctx, "t1/acme/widgets")
	if st.Revision != 1 {
		t.Fatalf("referee advanced on a rejected push: %+v", st)
	}
	m2, _, err := f.engine.LoadManifest(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Packs) != len(m.Packs) || m2.Ref("refs/heads/main").Target != m.Ref("refs/heads/main").Target {
		t.Fatalf("manifest changed on rejected push: %+v", m2)
	}
	if len(f.store.Keys()) != 3 { // manifest + pack + idx from the seed
		t.Fatalf("bucket grew on rejected push: %v", f.store.Keys())
	}
}

// TestPushDeletedRefRejectedByGuard: BranchRule.BlockForcePush semantics
// reject ref deletion (new == zero id) on the protected pattern.
func TestPushDeletedRefRejectedByGuard(t *testing.T) {
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	base := map[string]string{}
	for _, r := range m.Refs {
		base[r.Name] = r.Target
	}
	referee := newMemReferee(1, base)
	guard := &recordingGuard{reject: func(cmd PushCommand, actor string) error {
		if cmd.Ref == "refs/heads/main" && cmd.New == zeroID {
			return fmt.Errorf("deleting main is rejected by policy")
		}
		return nil
	}}
	f := newReceiveFixture(t, mem, referee, guard, nil)

	work := t.TempDir()
	clone := exec.Command("git", "clone", "-q", f.server.URL+"/git/acme/widgets", work)
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	out, err := gitPush(t, work, "origin", ":main")
	if err == nil {
		t.Fatalf("delete push must be rejected:\n%s", out)
	}
	if !strings.Contains(out, "remote rejected") || !strings.Contains(out, "deleting main") {
		t.Fatalf("delete rejection must carry the policy reason, got:\n%s", out)
	}
	st, _ := referee.Load(context.Background(), "t1/acme/widgets")
	if st.Revision != 1 {
		t.Fatalf("referee advanced on rejected delete: %+v", st)
	}
}

// TestPushLeaseHeld: when another instance holds the repo's push lease the
// request fails with 423 before any guard or receive-pack work, and the
// lease is eventually releasable.
func TestPushLeaseHeld(t *testing.T) {
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	base := map[string]string{}
	for _, r := range m.Refs {
		base[r.Name] = r.Target
	}
	referee := newMemReferee(1, base)
	leases := newMemLeases()
	ctx := context.Background()
	release, err := leases.Acquire(ctx, "t1/acme/widgets", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	guard := &recordingGuard{}
	f := newReceiveFixture(t, mem, referee, guard, leases)

	work := t.TempDir()
	clone := exec.Command("git", "clone", "-q", f.server.URL+"/git/acme/widgets", work)
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	writeFixtureCommit(t, work, "x", "new.txt", "new\n")
	gitCmd(t, work, "commit", "-qm", "blocked push")
	out, err := gitPush(t, work, "origin", "main")
	if err == nil {
		t.Fatalf("push with lease held must fail:\n%s", out)
	}
	if !strings.Contains(out, "423") {
		t.Fatalf("lease rejection must surface 423:\n%s", out)
	}

	// Guard never ran (lease precedes it) and the referee is untouched.
	if len(guard.cmds) != 0 {
		t.Fatalf("guard ran on a leased repo: %+v", guard.cmds)
	}
	st, _ := referee.Load(ctx, "t1/acme/widgets")
	if st.Revision != 1 {
		t.Fatalf("referee advanced on leased push: %+v", st)
	}

	// After release, the same push succeeds.
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if out, err := gitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push after release: %v\n%s", err, out)
	}
	st, _ = referee.Load(ctx, "t1/acme/widgets")
	if st.Revision != 2 {
		t.Fatalf("push after release did not commit: %+v", st)
	}
}

// TestPushRefereeDiverged: the referee's refs disagreeing with the hydrated
// manifest means another instance committed in between — the push must fail
// with 409 (fetch first) instead of committing on a divergent base.
func TestPushRefereeDiverged(t *testing.T) {
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	base := map[string]string{}
	for _, r := range m.Refs {
		base[r.Name] = r.Target
	}
	// Referee ahead of the manifest: another instance won a push.
	referee := newMemReferee(2, map[string]string{
		"refs/heads/main": strings.Repeat("a", 40),
		"refs/tags/v1.0":  base["refs/tags/v1.0"],
	})
	f := newReceiveFixture(t, mem, referee, nil, nil)

	work := t.TempDir()
	clone := exec.Command("git", "clone", "-q", f.server.URL+"/git/acme/widgets", work)
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	writeFixtureCommit(t, work, "x", "new.txt", "new\n")
	gitCmd(t, work, "commit", "-qm", "stale push")
	out, err := gitPush(t, work, "origin", "main")
	if err == nil {
		t.Fatalf("push against a divergent referee must fail:\n%s", out)
	}
	if !strings.Contains(out, "409") {
		t.Fatalf("divergence must surface 409:\n%s", out)
	}
	if referee.rev != 2 {
		t.Fatalf("referee advanced on divergent push: %+v", referee)
	}
}

// TestPushAdvertisementServed: GET /info/refs?service=git-receive-pack
// advertises refs only when the receive path is wired; read-only instances
// must refuse it with 400.
func TestPushAdvertisementServed(t *testing.T) {
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	base := map[string]string{}
	for _, r := range m.Refs {
		base[r.Name] = r.Target
	}
	referee := newMemReferee(1, base)
	f := newReceiveFixture(t, mem, referee, nil, nil)
	url := f.server.URL + "/git/acme/widgets/info/refs?service=git-receive-pack"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("receive advertisement = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-git-receive-pack-advertisement" {
		t.Fatalf("content type = %q", ct)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	body := buf.String()
	if !strings.Contains(body, "# service=git-receive-pack\n") {
		t.Fatalf("advertisement missing service header:\n%.200s", body)
	}
	// The seeded main ref is advertised with its target sha.
	mainRef := m.Ref("refs/heads/main")
	if !strings.Contains(body, mainRef.Target+" refs/heads/main") &&
		!strings.Contains(body, mainRef.Target+"\\x20refs/heads/main") {
		t.Fatalf("advertisement must list refs/heads/main at %s:\n%.200s", mainRef.Target, body)
	}

	// A read-only SmartHTTP must not expose receive at all.
	roMem := objectstore.NewMemStore()
	roCache := &RepoCache{Root: t.TempDir(), Engine: New(roMem), Packs: NewBucketPackSource(roMem)}
	ro := &SmartHTTP{Hydrate: func(ctx context.Context, owner, repo string) (string, error) {
		return roCache.Hydrate(ctx, "t1", owner, repo)
	}}
	roSrv := httptest.NewServer(http.StripPrefix("/git", http.Handler(ro)))
	defer roSrv.Close()
	resp2, err := http.Get(roSrv.URL + "/git/acme/widgets/info/refs?service=git-receive-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("read-only instance receive advertisement = %d, want 400", resp2.StatusCode)
	}
}

// pktFor builds one pkt-line from a payload.
func pktFor(s string) string {
	return fmt.Sprintf("%04x", len(s)+4) + s
}

func TestParsePushCommands(t *testing.T) {
	sha1 := strings.Repeat("1", 40)
	sha2 := strings.Repeat("2", 40)

	// Typical first push: one create command with capabilities, then flush,
	// then the pack payload.
	body := pktFor(zeroID+" "+sha1+" refs/heads/main\x00 report-status agent=git/2.40") +
		"0000" + "PACK...."
	cmds, caps, err := parsePushCommands([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Old != zeroID || cmds[0].New != sha1 || cmds[0].Ref != "refs/heads/main" {
		t.Fatalf("cmds = %+v", cmds)
	}
	if !strings.Contains(caps, "report-status") || !strings.Contains(caps, "agent=git/2.40") {
		t.Fatalf("caps = %q", caps)
	}

	// Multi-command push: update main, create feature, delete stale branch.
	body = pktFor(sha1+" "+sha2+" refs/heads/main\x00 report-status") +
		pktFor(zeroID+" "+sha1+" refs/heads/feature") +
		pktFor(sha2+" "+zeroID+" refs/heads/stale") +
		"0000" + "PACK...."
	cmds, _, err = parsePushCommands([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("cmds = %+v, want 3", cmds)
	}
	want := []PushCommand{
		{Old: sha1, New: sha2, Ref: "refs/heads/main"},
		{Old: zeroID, New: sha1, Ref: "refs/heads/feature"},
		{Old: sha2, New: zeroID, Ref: "refs/heads/stale"},
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Fatalf("cmd[%d] = %+v, want %+v", i, cmds[i], want[i])
		}
	}

	// Empty command list (push with nothing to do) is an error: the handler
	// must not commit state for a no-op stream.
	if _, _, err := parsePushCommands([]byte("0000PACK")); err == nil {
		t.Fatal("empty command list must be an error")
	}

	// Truncated stream: a pkt-line header promising more bytes than present.
	if _, _, err := parsePushCommands([]byte("0032sh")); err == nil {
		t.Fatal("truncated stream must be an error")
	}

	// Malformed command line (not old/new/ref).
	if _, _, err := parsePushCommands([]byte(pktFor("garbage\x00caps") + "0000")); err == nil {
		t.Fatal("malformed command must be an error")
	}

	// Push certificate prefix (sig) is refused — v0 plain commands only.
	if _, _, err := parsePushCommands([]byte(pktFor("push-cert\x00caps") + "0000")); err == nil {
		t.Fatal("push-cert must be refused")
	}
}
