package main

// The quill API client (2026-11-15): `quill pr …` and `quill run …` drive the
// deployed REST API with an API token (Bearer qat_…), so people and agents
// open, merge and close PRs and trigger/watch pipeline runs from a terminal
// without a browser session. Unlike `quill admin`, it needs no database or
// cloud credentials: only a token, which acts as its user and can be revoked
// from that user's token list.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultAPIURL = "https://api.black-tulip.nl"

// apiClient is a minimal JSON client for /api/v1/projects/{p}/repos/{r}.
type apiClient struct {
	base    string // API origin, no trailing slash
	token   string
	project string
	repo    string
	http    *http.Client
	out     io.Writer
	asJSON  bool
}

// clientFlags are shared by every client command.
type clientFlags struct {
	api, repo, tokenFile string
	json                 bool
}

func (f *clientFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.api, "api", "", "API origin (default $QUILL_API_URL or "+defaultAPIURL+")")
	fs.StringVar(&f.repo, "repo", "", "project/repo (default $QUILL_REPO, else inferred from git remotes)")
	fs.StringVar(&f.tokenFile, "token-file", "", "file holding the API token (default $QUILL_TOKEN, $QUILL_TOKEN_FILE, ~/.config/quill/token)")
	fs.BoolVar(&f.json, "json", false, "print the raw JSON response")
}

func (f *clientFlags) client() (*apiClient, error) {
	base := firstNonEmpty(f.api, os.Getenv("QUILL_API_URL"), defaultAPIURL)
	token, err := resolveToken(f.tokenFile)
	if err != nil {
		return nil, err
	}
	repoArg := firstNonEmpty(f.repo, os.Getenv("QUILL_REPO"))
	if repoArg == "" {
		repoArg = repoFromGitRemotes()
	}
	project, repo, ok := strings.Cut(strings.Trim(repoArg, "/"), "/")
	if !ok || project == "" || repo == "" {
		return nil, errors.New("which repository? pass --repo project/repo (or set QUILL_REPO, or run inside a clone of a Quill repo)")
	}
	return &apiClient{
		base: strings.TrimRight(base, "/"), token: token, project: project, repo: strings.TrimSuffix(repo, ".git"),
		http: &http.Client{Timeout: 60 * time.Second}, out: os.Stdout, asJSON: f.json,
	}, nil
}

// parseInterspersed parses flags wherever they appear (`pr merge 7 --method
// rebase` as well as `pr merge --method rebase 7`); positionals are kept in
// order and become fs.Args().
func parseInterspersed(fs *flag.FlagSet, args []string) error {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	return fs.Parse(append([]string{"--"}, pos...))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// resolveToken reads the API token: --token-file, $QUILL_TOKEN,
// $QUILL_TOKEN_FILE, then ~/.config/quill/token.
func resolveToken(file string) (string, error) {
	if file == "" {
		if t := strings.TrimSpace(os.Getenv("QUILL_TOKEN")); t != "" {
			return t, nil
		}
		file = os.Getenv("QUILL_TOKEN_FILE")
	}
	if file == "" {
		if home, err := os.UserHomeDir(); err == nil {
			file = filepath.Join(home, ".config", "quill", "token")
		}
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("no API token: set QUILL_TOKEN or QUILL_TOKEN_FILE, or write one to ~/.config/quill/token (%v)", err)
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", fmt.Errorf("API token file %s is empty", file)
	}
	return t, nil
}

// remoteRe matches a Quill git URL: https://git.<host>/<project>/<repo>(.git)
// or the legacy https://api.<host>/git/<project>/<repo>.
var remoteRe = regexp.MustCompile(`^https?://(?:[^@/]+@)?(?:git\.[^/]+|[^/]+/git)/([^/]+)/([^/]+?)(?:\.git)?/?$`)

// parseQuillRemote extracts project/repo from a Quill remote URL.
func parseQuillRemote(u string) (string, bool) {
	m := remoteRe.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return "", false
	}
	return m[1] + "/" + m[2], true
}

// repoFromGitRemotes picks the repository from the current clone's remotes,
// preferring one named "quill", then "origin", then any Quill URL.
func repoFromGitRemotes() string {
	out, err := exec.Command("git", "remote", "-v").Output()
	if err != nil {
		return ""
	}
	byName := map[string]string{}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if r, ok := parseQuillRemote(f[1]); ok {
			if _, seen := byName[f[0]]; !seen {
				names = append(names, f[0])
			}
			byName[f[0]] = r
		}
	}
	for _, n := range []string{"quill", "origin"} {
		if r, ok := byName[n]; ok {
			return r
		}
	}
	if len(names) > 0 {
		return byName[names[0]]
	}
	return ""
}

// call performs one request against the repo's API path and decodes JSON.
func (c *apiClient) call(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := fmt.Sprintf("%s/api/v1/projects/%s/repos/%s%s", c.base, url.PathEscape(c.project), url.PathEscape(c.repo), path)
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := firstNonEmpty(e.Message, strings.TrimSpace(string(raw)), resp.Status)
		return fmt.Errorf("%s %s: %d: %s", method, path, resp.StatusCode, msg)
	}
	if c.asJSON {
		var pretty bytes.Buffer
		if json.Indent(&pretty, raw, "", "  ") == nil {
			fmt.Fprintln(c.out, pretty.String())
		} else {
			fmt.Fprintln(c.out, string(raw))
		}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// ---- pull requests -----------------------------------------------------------

type cliPull struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	State        string `json:"state"`
	Merged       bool   `json:"merged"`
	Mergeable    bool   `json:"mergeable"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changedFiles"`
	Author       *struct {
		Login string `json:"login"`
	} `json:"author"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	MergeCommit string `json:"mergeCommitSha"`
}

func (p cliPull) status() string {
	switch {
	case p.Merged:
		return "merged"
	case p.State == "open" && !p.Mergeable:
		return "open (conflicts)"
	default:
		return p.State
	}
}

func (c *apiClient) printPull(p cliPull) {
	if c.asJSON {
		return
	}
	fmt.Fprintf(c.out, "#%d %s\n", p.Number, p.Title)
	fmt.Fprintf(c.out, "  %s · %s → %s · %s · +%d −%d in %d files\n", p.status(), p.Head.Ref, p.Base.Ref, short(p.Head.SHA), p.Additions, p.Deletions, p.ChangedFiles)
	if p.MergeCommit != "" {
		fmt.Fprintf(c.out, "  merge commit %s\n", short(p.MergeCommit))
	}
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func runPR(args []string) error {
	if len(args) == 0 {
		return errors.New("pr: missing subcommand (list, view, open, edit, merge, close, reopen)")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("pr "+sub, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var cf clientFlags
	cf.register(fs)
	state := fs.String("state", "open", "list: open, closed or all")
	head := fs.String("head", "", "open: source branch (default: the current branch)")
	base := fs.String("base", "main", "open: target branch")
	title := fs.String("title", "", "open/edit: title")
	body := fs.String("body", "", "open/edit: description (markdown)")
	bodyFile := fs.String("body-file", "", "open/edit: read the description from a file ('-' = stdin)")
	method := fs.String("method", "merge", "merge: merge, rebase or squash")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	c, err := cf.client()
	if err != nil {
		return err
	}
	number := func() (int, error) {
		n, err := strconv.Atoi(strings.TrimPrefix(fs.Arg(0), "#"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("pr %s: give the pull request number", sub)
		}
		return n, nil
	}
	readBody := func() (*string, error) {
		switch {
		case *bodyFile == "-":
			b, err := io.ReadAll(os.Stdin)
			s := string(b)
			return &s, err
		case *bodyFile != "":
			b, err := os.ReadFile(*bodyFile)
			s := string(b)
			return &s, err
		case *body != "":
			return body, nil
		}
		return nil, nil
	}
	var resp struct {
		Pull  cliPull   `json:"pull"`
		Pulls []cliPull `json:"pulls"`
	}
	switch sub {
	case "list":
		if err := c.call(http.MethodGet, "/pulls?state="+url.QueryEscape(*state), nil, &resp); err != nil {
			return err
		}
		if !c.asJSON {
			if len(resp.Pulls) == 0 {
				fmt.Fprintf(c.out, "no %s pull requests\n", *state)
			}
			for _, p := range resp.Pulls {
				fmt.Fprintf(c.out, "#%-4d %-16s %s (%s → %s)\n", p.Number, p.status(), p.Title, p.Head.Ref, p.Base.Ref)
			}
		}
		return nil
	case "view":
		n, err := number()
		if err != nil {
			return err
		}
		if err := c.call(http.MethodGet, fmt.Sprintf("/pulls/%d", n), nil, &resp); err != nil {
			return err
		}
		c.printPull(resp.Pull)
		if !c.asJSON && strings.TrimSpace(resp.Pull.Body) != "" {
			fmt.Fprintf(c.out, "\n%s\n", resp.Pull.Body)
		}
		return nil
	case "open":
		h := *head
		if h == "" {
			out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
			if err != nil {
				return errors.New("pr open: pass --head (not inside a git clone)")
			}
			h = strings.TrimSpace(string(out))
		}
		if strings.TrimSpace(*title) == "" {
			return errors.New("pr open: --title is required")
		}
		b, err := readBody()
		if err != nil {
			return err
		}
		req := map[string]string{"title": *title, "head": h, "base": *base, "body": ""}
		if b != nil {
			req["body"] = *b
		}
		if err := c.call(http.MethodPost, "/pulls", req, &resp); err != nil {
			return err
		}
		c.printPull(resp.Pull)
		return nil
	case "edit", "close", "reopen":
		n, err := number()
		if err != nil {
			return err
		}
		req := map[string]any{}
		switch sub {
		case "close":
			req["state"] = "closed"
		case "reopen":
			req["state"] = "open"
		default:
			if *title != "" {
				req["title"] = *title
			}
			b, err := readBody()
			if err != nil {
				return err
			}
			if b != nil {
				req["body"] = *b
			}
			if len(req) == 0 {
				return errors.New("pr edit: pass --title and/or --body/--body-file")
			}
		}
		if err := c.call(http.MethodPatch, fmt.Sprintf("/pulls/%d", n), req, &resp); err != nil {
			return err
		}
		c.printPull(resp.Pull)
		return nil
	case "merge":
		n, err := number()
		if err != nil {
			return err
		}
		if err := c.call(http.MethodPost, fmt.Sprintf("/pulls/%d/merge", n), map[string]string{"method": *method}, &resp); err != nil {
			return err
		}
		c.printPull(resp.Pull)
		return nil
	default:
		return fmt.Errorf("pr: unknown subcommand %q (list, view, open, edit, merge, close, reopen)", sub)
	}
}

// ---- pipeline runs -------------------------------------------------------------

type cliRun struct {
	RunNumber     int    `json:"runNumber"`
	WorkflowPath  string `json:"workflowPath"`
	Status        string `json:"status"`
	Event         string `json:"event"`
	Ref           string `json:"ref"`
	CommitSha     string `json:"commitSha"`
	BlockedReason string `json:"blockedReason"`
	CreatedAt     string `json:"createdAt"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt"`
	Jobs          []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		StartedAt  string `json:"startedAt"`
		FinishedAt string `json:"finishedAt"`
		Steps      []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Logs       string `json:"logs"`
			StartedAt  string `json:"startedAt"`
			FinishedAt string `json:"finishedAt"`
		} `json:"steps"`
	} `json:"jobs"`
}

// span renders the time between two RFC3339 stamps ("" when the start is
// missing or unparseable). An empty end means "still going": measured to now.
func span(from, to string) string {
	a, err := time.Parse(time.RFC3339Nano, from)
	if err != nil {
		return ""
	}
	b := time.Now()
	if to != "" {
		if b, err = time.Parse(time.RFC3339Nano, to); err != nil {
			return ""
		}
	}
	d := b.Sub(a).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String()
}

func runDone(status string) bool {
	switch status {
	case "success", "failure", "cancelled", "skipped":
		return true
	}
	return false
}

func runRun(args []string) error {
	if len(args) == 0 {
		return errors.New("run: missing subcommand (list, view, cancel, trigger, watch)")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("run "+sub, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var cf clientFlags
	cf.register(fs)
	workflow := fs.String("workflow", "", "workflow path, e.g. .github/workflows/deploy.yml")
	ref := fs.String("ref", "main", "trigger: branch to run on")
	env := fs.String("environment", "", "trigger: target environment (optional)")
	limit := fs.Int("limit", 15, "list: number of runs")
	logs := fs.Bool("logs", false, "view: print step logs (every step that ran; also while the run is going)")
	interval := fs.Duration("interval", 15*time.Second, "watch: poll interval")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	c, err := cf.client()
	if err != nil {
		return err
	}
	runNumber := func() (int, error) {
		n, err := strconv.Atoi(strings.TrimPrefix(fs.Arg(0), "#"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("run %s: give the run number", sub)
		}
		if *workflow == "" {
			return 0, fmt.Errorf("run %s: --workflow is required (run numbers count per workflow)", sub)
		}
		return n, nil
	}
	get := func(n int) (cliRun, error) {
		var resp struct {
			Run cliRun `json:"run"`
		}
		err := c.call(http.MethodGet, fmt.Sprintf("/pipelines/runs/%d?workflow=%s", n, url.QueryEscape(*workflow)), nil, &resp)
		return resp.Run, err
	}
	printRun := func(r cliRun, withLogs, every bool) {
		fmt.Fprintf(c.out, "#%d %s · %s · %s %s @ %s\n", r.RunNumber, r.WorkflowPath, r.Status, r.Event, strings.TrimPrefix(r.Ref, "refs/heads/"), short(r.CommitSha))
		if r.BlockedReason != "" {
			fmt.Fprintf(c.out, "  blocked: %s\n", r.BlockedReason)
		}
		// Queue wait vs. run time tells a slow run from one that never started.
		switch {
		case r.StartedAt == "" && !runDone(r.Status):
			fmt.Fprintf(c.out, "  queued for %s, not started\n", span(r.CreatedAt, ""))
		case r.StartedAt != "":
			fmt.Fprintf(c.out, "  queued %s, ran %s\n", span(r.CreatedAt, r.StartedAt), span(r.StartedAt, r.FinishedAt))
		}
		if r.StartedAt != "" && !runDone(r.Status) && len(r.Jobs) == 0 {
			fmt.Fprintln(c.out, "  no jobs yet (the workflow has not loaded)")
		}
		for _, j := range r.Jobs {
			fmt.Fprintf(c.out, "  job %s: %s %s\n", j.Name, j.Status, span(j.StartedAt, j.FinishedAt))
			for _, s := range j.Steps {
				notOK := s.Status != "success" && s.Status != "skipped"
				if !notOK && !every {
					continue
				}
				fmt.Fprintf(c.out, "    %s: %s %s\n", s.Name, s.Status, span(s.StartedAt, s.FinishedAt))
				// --logs shows the tail of every step that ran, so a slow step
				// can be told from a hung one while the run is still going.
				if withLogs && (every || s.Status == "failure") && s.Status != "skipped" && strings.TrimSpace(s.Logs) != "" {
					fmt.Fprintln(c.out, indent(indent(tail(s.Logs, 60))))
				}
			}
		}
	}
	switch sub {
	case "list":
		var resp struct {
			Runs []cliRun `json:"runs"`
		}
		if err := c.call(http.MethodGet, "/pipelines/runs", nil, &resp); err != nil {
			return err
		}
		if !c.asJSON {
			for i, r := range resp.Runs {
				if i >= *limit {
					break
				}
				if *workflow != "" && r.WorkflowPath != *workflow {
					continue
				}
				fmt.Fprintf(c.out, "#%-4d %-10s %-38s %s %s\n", r.RunNumber, r.Status, r.WorkflowPath, strings.TrimPrefix(r.Ref, "refs/heads/"), short(r.CommitSha))
			}
		}
		return nil
	case "view":
		n, err := runNumber()
		if err != nil {
			return err
		}
		r, err := get(n)
		if err != nil {
			return err
		}
		if !c.asJSON {
			printRun(r, *logs, *logs)
		}
		return nil
	case "cancel":
		n, err := runNumber()
		if err != nil {
			return err
		}
		if err := c.call(http.MethodPost, fmt.Sprintf("/pipelines/runs/%d/cancel?workflow=%s", n, url.QueryEscape(*workflow)), nil, nil); err != nil {
			return err
		}
		if !c.asJSON {
			fmt.Fprintf(c.out, "cancelled %s #%d\n", *workflow, n)
		}
		return nil
	case "trigger":
		if *workflow == "" {
			return errors.New("run trigger: --workflow is required")
		}
		req := map[string]string{"workflow": *workflow, "ref": *ref}
		if *env != "" {
			req["environment"] = *env
		}
		var resp struct {
			Run cliRun `json:"run"`
		}
		if err := c.call(http.MethodPost, "/pipelines", req, &resp); err != nil {
			return err
		}
		if !c.asJSON {
			fmt.Fprintf(c.out, "triggered %s #%d on %s (%s)\n", *workflow, resp.Run.RunNumber, *ref, resp.Run.Status)
		}
		return nil
	case "watch":
		n, err := runNumber()
		if err != nil {
			return err
		}
		c.asJSON = false
		last := ""
		for {
			r, err := get(n)
			if err != nil {
				return err
			}
			if r.Status != last {
				fmt.Fprintf(c.out, "%s #%d: %s\n", time.Now().Format("15:04:05"), n, r.Status)
				last = r.Status
			}
			if runDone(r.Status) {
				if r.Status != "success" {
					printRun(r, true, false)
					return fmt.Errorf("run #%d %s", n, r.Status)
				}
				return nil
			}
			time.Sleep(*interval)
		}
	default:
		return fmt.Errorf("run: unknown subcommand %q (list, view, cancel, trigger, watch)", sub)
	}
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
