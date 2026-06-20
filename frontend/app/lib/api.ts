// Server-side API client for the Quill backend.
//
// Browser code should call the rewrite at /api/backend/* (see next.config.mjs);
// server components use QUILL_API_BASE_URL directly. Keep all backend response
// types defined here so pages stay decoupled from fetch details.

const API_BASE = process.env.QUILL_API_BASE_URL || "http://localhost:8080";

export type ForgejoStatus = {
  configured: boolean;
  reachable: boolean;
  version?: string;
  publicUrl?: string;
};

export type Meta = {
  name: string;
  version: string;
  env: string;
  forgejo?: ForgejoStatus;
};

export type Project = {
  id: string;
  slug: string;
  name: string;
  description: string;
  forgejoOrg?: string;
  createdAt: string;
};

export type Repo = {
  id: string;
  slug: string;
  name: string;
  description: string;
  visibility: string;
  defaultBranch: string;
  isArchived: boolean;
  forgejoOwner?: string;
  forgejoName?: string;
  starCount: number;
  viewerHasStarred: boolean;
  createdAt: string;
};

export type Branch = {
  name: string;
  protected: boolean;
  commitSha: string;
  commitMessage: string;
  commitDate: string;
};

export type Commit = {
  sha: string;
  message: string;
  authorName: string;
  authorLogin?: string;
  date: string;
};

export type ContentEntry = {
  name: string;
  path: string;
  type: "file" | "dir" | "symlink" | "submodule";
  size: number;
};

export type ContentFile = {
  name: string;
  path: string;
  sha: string;
  size: number;
  isBinary: boolean;
  tooLarge: boolean;
  content?: string;
};

export type Contents = {
  type: "dir" | "file";
  path: string;
  entries?: ContentEntry[];
  file?: ContentFile;
};

export type UserRef = { login: string; name?: string };

export type PullRef = { label: string; ref: string; sha: string };

export type PullRequest = {
  number: number;
  title: string;
  body: string;
  state: string;
  draft: boolean;
  merged: boolean;
  mergeable: boolean;
  comments: number;
  additions: number;
  deletions: number;
  changedFiles: number;
  author: UserRef | null;
  head: PullRef;
  base: PullRef;
  htmlUrl: string;
  createdAt: string;
  updatedAt: string;
  mergedAt?: string;
  mergedBy?: UserRef;
  mergeCommitSha?: string;
  viewerIsAuthor: boolean;
};

export type PullComment = {
  id: number;
  body: string;
  author: UserRef | null;
  createdAt: string;
};

export type ReviewState = "APPROVED" | "REQUEST_CHANGES" | "COMMENT" | "PENDING";

export type Review = {
  id: number;
  state: ReviewState;
  body: string;
  author: UserRef | null;
  stale: boolean;
  dismissed: boolean;
  submittedAt: string;
};

// PolicyGate is the merge verdict for a PR against the policy on its base branch.
export type PolicyGate = {
  applies: boolean;
  pattern?: string;
  requiredApprovals: number;
  approvals: number;
  changesRequested: number;
  blocked: boolean;
  reason?: string;
  denials?: PolicyDenial[];
  requireStatusChecks?: boolean;
  allChecksPass?: boolean;
  checkCount?: number;
};

// PolicyDenial is one scope-tagged reason the composed gate blocks merging, used
// to explain which scope (tenant/project/repo) set the rule that failed.
export type PolicyDenial = {
  scope: string;
  selector: string;
  message: string;
};

// PolicyScope is the level a branch policy is declared at. A repo inherits the
// policies of its project and tenant.
export type PolicyScope = "repo" | "project" | "tenant";

// BranchPolicy is a Quill-owned branch-protection rule. scope says which level
// declared it; locked marks a floor that narrower scopes may only tighten.
export type BranchPolicy = {
  scope?: PolicyScope;
  pattern: string;
  requiredApprovals: number;
  dismissStaleApprovals: boolean;
  requireUpToDate: boolean;
  blockForcePush: boolean;
  requirePullRequest: boolean;
  requireStatusChecks: boolean;
  locked?: boolean;
  updatedAt: string;
};

export type DiffLine = {
  type: "context" | "add" | "del";
  content: string;
  oldNumber: number;
  newNumber: number;
};

export type DiffHunk = { header: string; lines: DiffLine[] };

export type DiffFile = {
  path: string;
  oldPath: string;
  status: string;
  isBinary: boolean;
  additions: number;
  deletions: number;
  hunks: DiffHunk[];
};

export type User = {
  id: string;
  username: string;
  email: string;
  displayName: string;
  isAdmin: boolean;
  isActive: boolean;
  createdAt: string;
};

export type AuthOk = { ok: true; token: string; user: User };
export type AuthErr = { ok: false; error: string };
export type AuthResult = AuthOk | AuthErr;

export type RegisterInput = {
  username: string;
  email: string;
  displayName?: string;
  password: string;
};

// getMeta fetches backend metadata. Returns null if the backend is unreachable
// so pages can render a degraded state instead of crashing.
export async function getMeta(): Promise<Meta | null> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/meta`, { cache: "no-store" });
    if (!res.ok) return null;
    return (await res.json()) as Meta;
  } catch {
    return null;
  }
}

// login exchanges credentials for a token + user. Network and auth failures are
// returned as { ok: false } so callers can surface a friendly message.
export async function login(
  username: string,
  password: string,
): Promise<AuthResult> {
  return postAuth("/api/v1/auth/login", { username, password });
}

// register creates an account and returns a token + user (the first account
// created becomes an admin).
export async function register(input: RegisterInput): Promise<AuthResult> {
  return postAuth("/api/v1/auth/register", input);
}

// fetchMe resolves the current user for a token, or null if it is missing/invalid.
export async function fetchMe(token: string): Promise<User | null> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/auth/me`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return null;
    return (await res.json()) as User;
  } catch {
    return null;
  }
}

// listProjects returns all projects visible to the authenticated user, or an
// empty list when the backend is unreachable so pages can render a degraded state.
export async function listProjects(token: string): Promise<Project[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/projects`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { projects?: Project[] };
    return data.projects ?? [];
  } catch {
    return [];
  }
}

// listReposByProject returns the repositories within an project, or an empty list on error.
export async function listReposByProject(
  token: string,
  slug: string,
): Promise<Repo[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/projects/${slug}/repos`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { repositories?: Repo[] };
    return data.repositories ?? [];
  } catch {
    return [];
  }
}

// getOpenPullRequestCount returns the total number of open pull requests across
// every repository the user can see, computed server-side in a single request.
// Returns 0 on any failure so the dashboard degrades gracefully.
export async function getOpenPullRequestCount(token: string): Promise<number> {
  const res = await authGet<{ openPullRequests: number }>(
    token,
    "/api/v1/me/pulls/open-count",
  );
  return res.ok ? res.data.openPullRequests : 0;
}

// GitCredential is a one-time git-over-HTTPS credential: a username and a freshly
// minted access token used as the password when cloning or pushing. id
// identifies the stored token so it can be revoked later.
export type GitCredential = { id: string; username: string; token: string };

// GitTokenSummary is the metadata for an outstanding git token; the secret is
// never returned after creation.
export type GitTokenSummary = { id: string; name: string; createdAt: string };

// createGitToken mints a personal git access token for the user (shown once).
// name is an optional user-facing label.
export function createGitToken(
  token: string,
  name?: string,
): Promise<DataResult<GitCredential>> {
  return postData<GitCredential>(token, "/api/v1/me/git-token", { name: name ?? "" });
}

// listGitTokens returns the user's outstanding git tokens (metadata only).
export async function listGitTokens(
  token: string,
): Promise<Result<GitTokenSummary[]>> {
  return authGet<GitTokenSummary[]>(token, "/api/v1/me/git-tokens");
}

// revokeGitToken revokes one of the user's git tokens by id.
export function revokeGitToken(
  token: string,
  id: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/me/git-tokens/${id}`);
}

// SSHKey is a public SSH key registered for the user.
export type SSHKey = {
  id: number;
  title: string;
  key: string;
  fingerprint: string;
};

// listSSHKeys returns the user's SSH public keys from the git backend.
export async function listSSHKeys(
  token: string,
): Promise<Result<SSHKey[]>> {
  const res = await authGet<{ keys: SSHKey[] }>(token, "/api/v1/me/ssh-keys");
  if (!res.ok) return res;
  return { ok: true, data: res.data.keys };
}

// addSSHKey registers a new SSH public key for the user.
export function addSSHKey(
  token: string,
  title: string,
  key: string,
): Promise<DataResult<SSHKey>> {
  return postData<SSHKey>(token, "/api/v1/me/ssh-keys", { title, key });
}

// deleteSSHKey removes an SSH key by its Forgejo key ID.
export function deleteSSHKey(
  token: string,
  id: number,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/me/ssh-keys/${id}`);
}

async function postAuth(path: string, body: unknown): Promise<AuthResult> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as
      | { token?: string; user?: User; message?: string }
      | null;
    if (!res.ok || !data?.token || !data?.user) {
      return { ok: false, error: data?.message || "Authentication failed." };
    }
    return { ok: true, token: data.token, user: data.user };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// Result distinguishes success from a failed fetch by HTTP status so pages can
// render the right state (404 → not found, 403 → no access, etc.).
export type Result<T> =
  | { ok: true; data: T }
  | { ok: false; status: number; message: string };

// authGet performs an authenticated GET and decodes JSON, mapping transport and
// HTTP errors into a typed Result. status 0 indicates the backend is unreachable.
async function authGet<T>(token: string, path: string): Promise<Result<T>> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) {
      const body = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return {
        ok: false,
        status: res.status,
        message: body?.message || `Request failed (${res.status}).`,
      };
    }
    return { ok: true, data: (await res.json()) as T };
  } catch {
    return { ok: false, status: 0, message: "Can't reach the Quill backend." };
  }
}

// getProject fetches a single project.
export function getProject(token: string, slug: string): Promise<Result<Project>> {
  return authGet<Project>(token, `/api/v1/projects/${slug}`);
}

// reposResult is the project-detail repository listing payload.
export type ReposResult = { project: Project; repositories: Repo[] };

// getReposByProject returns an project plus its repositories, preserving HTTP status.
export function getReposByProject(
  token: string,
  slug: string,
): Promise<Result<ReposResult>> {
  return authGet<ReposResult>(token, `/api/v1/projects/${slug}/repos`);
}

// getRepo fetches a single repository's metadata.
export function getRepo(
  token: string,
  project: string,
  repo: string,
): Promise<Result<Repo>> {
  return authGet<Repo>(token, `/api/v1/projects/${project}/repos/${repo}`);
}

// branchesResult is the branch listing payload.
export type BranchesResult = {
  repository: Repo;
  defaultBranch: string;
  branches: Branch[];
};

export function getBranches(
  token: string,
  project: string,
  repo: string,
): Promise<Result<BranchesResult>> {
  return authGet<BranchesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/branches`,
  );
}

// commitsResult is the commit log payload.
export type CommitsResult = { repository: Repo; commits: Commit[] };

export function getCommits(
  token: string,
  project: string,
  repo: string,
  ref?: string,
  path?: string,
  limit = 30,
): Promise<Result<CommitsResult>> {
  const q = new URLSearchParams();
  if (ref) q.set("ref", ref);
  if (path) q.set("path", path);
  q.set("limit", String(limit));
  return authGet<CommitsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/commits?${q.toString()}`,
  );
}

// commitDetailResult is a single commit's metadata plus its parsed diff.
export type CommitDetailResult = {
  repository: Repo;
  commit: Commit;
  files: DiffFile[];
};

// getCommit returns a single commit's metadata and the diff it introduced.
export function getCommit(
  token: string,
  project: string,
  repo: string,
  sha: string,
): Promise<Result<CommitDetailResult>> {
  return authGet<CommitDetailResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/commits/${sha}`,
  );
}

// contentsResult is the directory/file contents payload.
export type ContentsResult = { repository: Repo; contents: Contents };

export function getContents(
  token: string,
  project: string,
  repo: string,
  path?: string,
  ref?: string,
): Promise<Result<ContentsResult>> {
  const q = new URLSearchParams();
  if (path) q.set("path", path);
  if (ref) q.set("ref", ref);
  const qs = q.toString();
  return authGet<ContentsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/contents${qs ? `?${qs}` : ""}`,
  );
}

// renderMarkdown renders markdown text to sanitized HTML in the repository's
// context (so relative links and references resolve). Returns null on failure so
// callers can fall back to plain text.
export async function renderMarkdown(
  token: string,
  project: string,
  repo: string,
  text: string,
): Promise<string | null> {
  const res = await postData<{ html: string }>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/markup`,
    { text },
  );
  return res.ok ? res.data.html : null;
}

// MutationResult reports the outcome of a create call: the created slug on
// success, or a human-readable error.
export type MutationResult =
  | { ok: true; slug: string }
  | { ok: false; error: string };

async function postCreate(
  token: string,
  path: string,
  body: unknown,
): Promise<MutationResult> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as
      | { slug?: string; message?: string }
      | null;
    if (!res.ok || !data?.slug) {
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true, slug: data.slug };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// createProject provisions an project (and its Forgejo mirror).
export function createProject(
  token: string,
  input: { slug: string; name: string; description: string },
): Promise<MutationResult> {
  return postCreate(token, "/api/v1/projects", input);
}

// createRepo provisions a repository under an project (and its Forgejo mirror).
export function createRepo(
  token: string,
  project: string,
  input: {
    slug: string;
    name: string;
    description: string;
    visibility: string;
  },
): Promise<MutationResult> {
  return postCreate(token, `/api/v1/projects/${project}/repos`, input);
}

// UpdateRepoInput is a partial repository update. Only the provided fields change;
// setting `slug` renames the repository (and its Forgejo git repo).
export type UpdateRepoInput = {
  name?: string;
  description?: string;
  visibility?: string;
  defaultBranch?: string;
  slug?: string;
  archived?: boolean;
};

// updateRepo changes a repository's general settings (project owners / admins only)
// and returns the repository's new state.
export function updateRepo(
  token: string,
  project: string,
  repo: string,
  input: UpdateRepoInput,
): Promise<DataResult<Repo>> {
  return sendData<Repo>(token, "PATCH", `/api/v1/projects/${project}/repos/${repo}`, input);
}

// deleteRepo permanently removes a repository (project owners / admins only).
export function deleteRepo(
  token: string,
  project: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/projects/${project}/repos/${repo}`);
}

// forkRepo forks a repository into a target project with an optional new slug.
export function forkRepo(
  token: string,
  project: string,
  repo: string,
  input: { targetProject: string; slug: string },
): Promise<DataResult<{ repository: Repo }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/fork`, input);
}

// starRepo records that the current user has starred a repository.
export function starRepo(
  token: string,
  project: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return putResource(token, `/api/v1/projects/${project}/repos/${repo}/star`);
}

// unstarRepo removes the current user's star from a repository.
export function unstarRepo(
  token: string,
  project: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/projects/${project}/repos/${repo}/star`);
}

// ---- pull requests ---------------------------------------------------------

// pullsResult is the PR listing payload.
export type PullsResult = { repository: Repo; pulls: PullRequest[] };

// getPulls returns a repository's pull requests filtered by state.
export function getPulls(
  token: string,
  project: string,
  repo: string,
  state: "open" | "closed" | "all" = "open",
): Promise<Result<PullsResult>> {
  return authGet<PullsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls?state=${state}`,
  );
}

// RepoPull is one entry in the cross-repository pull-request overview: a pull
// request together with the project/repo it belongs to, so the row can link back.
export type RepoPull = {
  projectSlug: string;
  repoSlug: string;
  repoName: string;
  pull: PullRequest;
};

// MyPullsResult is the cross-repository overview payload.
export type MyPullsResult = { pulls: RepoPull[] };

// getMyPulls returns open pull requests across every repository the signed-in
// user can access, newest-updated first. Optional cheap filters: state and project.
export function getMyPulls(
  token: string,
  opts: { state?: "open" | "closed" | "all"; project?: string } = {},
): Promise<Result<MyPullsResult>> {
  const q = new URLSearchParams();
  if (opts.state) q.set("state", opts.state);
  if (opts.project) q.set("project", opts.project);
  const suffix = q.toString() ? `?${q.toString()}` : "";
  return authGet<MyPullsResult>(token, `/api/v1/me/pulls${suffix}`);
}

// pullResult is the single-PR payload.
export type PullResult = { repository: Repo; pull: PullRequest };

export function getPull(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<PullResult>> {
  return authGet<PullResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}`,
  );
}

// diffResult is a PR's parsed diff payload.
export type DiffResult = { files: DiffFile[] };

export function getPullDiff(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<DiffResult>> {
  return authGet<DiffResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/diff`,
  );
}

// commentsResult is a PR's conversation payload.
export type CommentsResult = { comments: PullComment[] };

export function getPullComments(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<CommentsResult>> {
  return authGet<CommentsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/comments`,
  );
}

// reviewsResult carries a PR's reviews and the policy gate for its base branch.
export type ReviewsResult = { reviews: Review[]; gate: PolicyGate };

export function getPullReviews(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<ReviewsResult>> {
  return authGet<ReviewsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/reviews`,
  );
}

// policiesResult is the branch-policy listing payload.
export type PoliciesResult = {
  repository: Repo;
  policies: BranchPolicy[];
  inherited: BranchPolicy[];
};

export function getBranchPolicies(
  token: string,
  project: string,
  repo: string,
): Promise<Result<PoliciesResult>> {
  return authGet<PoliciesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/policies`,
  );
}

// DataResult carries a decoded body on success or a message on failure, for
// mutations that return a resource rather than a slug.
export type DataResult<T> = { ok: true; data: T } | { ok: false; error: string };

async function postData<T>(
  token: string,
  path: string,
  body: unknown,
): Promise<DataResult<T>> {
  return sendData<T>(token, "POST", path, body);
}

async function sendData<T>(
  token: string,
  method: "POST" | "PUT" | "PATCH",
  path: string,
  body: unknown,
): Promise<DataResult<T>> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as
      | (T & { message?: string })
      | null;
    if (!res.ok || !data) {
      return {
        ok: false,
        error: data?.message || `Request failed (${res.status}).`,
      };
    }
    return { ok: true, data: data as T };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// putResource issues an authenticated PUT with no body, treating any 2xx as success.
async function putResource(
  token: string,
  path: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "PUT",
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// deleteResource issues an authenticated DELETE, treating any 2xx as success.
async function deleteResource(
  token: string,
  path: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// createPull opens a pull request from head into base.
export function createPull(
  token: string,
  project: string,
  repo: string,
  input: { title: string; body: string; head: string; base: string },
): Promise<DataResult<{ pull: PullRequest }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/pulls`, input);
}

// createPullComment adds a comment to a pull request's conversation.
export function createPullComment(
  token: string,
  project: string,
  repo: string,
  number: number,
  body: string,
): Promise<DataResult<{ comment: PullComment }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/comments`,
    { body },
  );
}

// mergePull merges a pull request using the given method.
export function mergePull(
  token: string,
  project: string,
  repo: string,
  number: number,
  method: "merge" | "squash" | "rebase",
): Promise<DataResult<{ pull: PullRequest }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/merge`,
    { method },
  );
}

// createPullReview submits a review (approve, request changes, or comment).
export function createPullReview(
  token: string,
  project: string,
  repo: string,
  number: number,
  input: { event: ReviewState; body: string },
): Promise<DataResult<{ review: Review }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/reviews`,
    input,
  );
}

// getPullCommits returns the commits contained in a pull request.
export function getPullCommits(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<{ commits: Commit[] }>> {
  return authGet<{ commits: Commit[] }>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/commits`,
  );
}

// LineComment is a line-anchored review comment on a pull request's diff. line is
// the line number in the new version of the file.
export type LineComment = {
  id: number;
  path: string;
  line: number;
  body: string;
  author?: string;
  createdAt: string;
};

// getLineComments returns a pull request's line-anchored review comments.
export function getLineComments(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<{ comments: LineComment[] }>> {
  return authGet<{ comments: LineComment[] }>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/line-comments`,
  );
}

// createLineComment posts a single line-anchored comment on a PR's diff.
export function createLineComment(
  token: string,
  project: string,
  repo: string,
  number: number,
  input: { path: string; line: number; body: string },
): Promise<DataResult<{ comment: LineComment }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/line-comments`,
    input,
  );
}

// ---- branch policies -------------------------------------------------------

export type BranchPolicyInput = {
  pattern: string;
  requiredApprovals: number;
  dismissStaleApprovals: boolean;
  requireUpToDate: boolean;
  blockForcePush: boolean;
  requirePullRequest: boolean;
  requireStatusChecks: boolean;
  locked?: boolean;
};

// setBranchPolicy creates or updates a repo branch policy (project owners / admins only).
export function setBranchPolicy(
  token: string,
  project: string,
  repo: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/repos/${repo}/policies`, input);
}

// deleteBranchPolicy removes the repo policy for a branch pattern.
export function deleteBranchPolicy(
  token: string,
  project: string,
  repo: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/repos/${repo}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- project-scoped policies ----------------------------------------------

export type ProjectPoliciesResult = {
  project: Project;
  policies: BranchPolicy[];
  inherited: BranchPolicy[];
};

// getProjectPolicies returns a project's own branch policies plus the ones it
// inherits from its tenant. Open to project members.
export function getProjectPolicies(
  token: string,
  project: string,
): Promise<Result<ProjectPoliciesResult>> {
  return authGet<ProjectPoliciesResult>(token, `/api/v1/projects/${project}/policies`);
}

// setProjectPolicy creates or updates a project-scoped branch policy that applies
// to every repository in the project (project owners / admins only).
export function setProjectPolicy(
  token: string,
  project: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/policies`, input);
}

// deleteProjectPolicy removes a project-scoped branch policy.
export function deleteProjectPolicy(
  token: string,
  project: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- tenant-scoped policies (platform admins only) ------------------------

export type TenantPoliciesResult = {
  tenant: { slug: string; name: string };
  policies: BranchPolicy[];
};

// getTenantPolicies returns a tenant's own branch policies (platform admins only).
export function getTenantPolicies(
  token: string,
  tenant: string,
): Promise<Result<TenantPoliciesResult>> {
  return authGet<TenantPoliciesResult>(token, `/api/v1/tenants/${tenant}/policies`);
}

// setTenantPolicy creates or updates a tenant-scoped branch policy that applies
// to every project and repository in the tenant (platform admins only).
export function setTenantPolicy(
  token: string,
  tenant: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/tenants/${tenant}/policies`, input);
}

// deleteTenantPolicy removes a tenant-scoped branch policy.
export function deleteTenantPolicy(
  token: string,
  tenant: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/tenants/${tenant}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- environment policies --------------------------------------------------

// EnvironmentPolicy is a Quill-owned deploy gate for an environment (name or
// glob). scope says which level declared it; locked marks a floor that narrower
// scopes may only tighten. The rule fields answer the deploy-control asks:
// required approvers, which source branches may deploy, an ordered promotion
// path, a required green run, and a soak/freeze window.
export type EnvironmentPolicy = {
  scope?: PolicyScope;
  pattern: string;
  requiredApprovals: number;
  allowedSourceBranches: string[];
  requirePreviousEnvironment: string;
  requireSuccessfulRun: boolean;
  minWaitMinutes: number;
  locked?: boolean;
  updatedAt: string;
};

export type EnvironmentPolicyInput = {
  pattern: string;
  requiredApprovals: number;
  allowedSourceBranches: string[];
  requirePreviousEnvironment: string;
  requireSuccessfulRun: boolean;
  minWaitMinutes: number;
  locked?: boolean;
};

// ---- repo-scoped environment policies -------------------------------------

export type EnvironmentPoliciesResult = {
  repository: Repo;
  policies: EnvironmentPolicy[];
  inherited: EnvironmentPolicy[];
};

// getEnvironmentPolicies returns a repo's own environment policies plus the ones
// it inherits from its project and tenant.
export function getEnvironmentPolicies(
  token: string,
  project: string,
  repo: string,
): Promise<Result<EnvironmentPoliciesResult>> {
  return authGet<EnvironmentPoliciesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/environment-policies`,
  );
}

// setEnvironmentPolicy creates or updates a repo environment policy (project
// owners / admins only).
export function setEnvironmentPolicy(
  token: string,
  project: string,
  repo: string,
  input: EnvironmentPolicyInput,
): Promise<DataResult<{ policy: EnvironmentPolicy }>> {
  return sendData(
    token,
    "PUT",
    `/api/v1/projects/${project}/repos/${repo}/environment-policies`,
    input,
  );
}

// deleteEnvironmentPolicy removes the repo policy for an environment selector.
export function deleteEnvironmentPolicy(
  token: string,
  project: string,
  repo: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/repos/${repo}/environment-policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- project-scoped environment policies ----------------------------------

export type ProjectEnvironmentPoliciesResult = {
  project: Project;
  policies: EnvironmentPolicy[];
  inherited: EnvironmentPolicy[];
};

// getProjectEnvironmentPolicies returns a project's own environment policies plus
// the ones it inherits from its tenant. Open to project members.
export function getProjectEnvironmentPolicies(
  token: string,
  project: string,
): Promise<Result<ProjectEnvironmentPoliciesResult>> {
  return authGet<ProjectEnvironmentPoliciesResult>(
    token,
    `/api/v1/projects/${project}/environment-policies`,
  );
}

// setProjectEnvironmentPolicy creates or updates a project-scoped environment
// policy that applies to every repository in the project (project owners /
// admins only).
export function setProjectEnvironmentPolicy(
  token: string,
  project: string,
  input: EnvironmentPolicyInput,
): Promise<DataResult<{ policy: EnvironmentPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/environment-policies`, input);
}

// deleteProjectEnvironmentPolicy removes a project-scoped environment policy.
export function deleteProjectEnvironmentPolicy(
  token: string,
  project: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/environment-policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- tenant-scoped environment policies (platform admins only) ------------

export type TenantEnvironmentPoliciesResult = {
  tenant: { slug: string; name: string };
  policies: EnvironmentPolicy[];
};

// getTenantEnvironmentPolicies returns a tenant's own environment policies
// (platform admins only).
export function getTenantEnvironmentPolicies(
  token: string,
  tenant: string,
): Promise<Result<TenantEnvironmentPoliciesResult>> {
  return authGet<TenantEnvironmentPoliciesResult>(
    token,
    `/api/v1/tenants/${tenant}/environment-policies`,
  );
}

// setTenantEnvironmentPolicy creates or updates a tenant-scoped environment
// policy that applies to every project and repository in the tenant (platform
// admins only).
export function setTenantEnvironmentPolicy(
  token: string,
  tenant: string,
  input: EnvironmentPolicyInput,
): Promise<DataResult<{ policy: EnvironmentPolicy }>> {
  return sendData(token, "PUT", `/api/v1/tenants/${tenant}/environment-policies`, input);
}

// deleteTenantEnvironmentPolicy removes a tenant-scoped environment policy.
export function deleteTenantEnvironmentPolicy(
  token: string,
  tenant: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/tenants/${tenant}/environment-policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- environments ----------------------------------------------------------

// Environment is a project-owned, ranked deployment target (staging, production,
// …). rank orders the promotion ladder (lower deploys first). The slug is the
// stable identifier matched by environment-policy selectors.
export type Environment = {
  id: string;
  slug: string;
  name: string;
  description: string;
  rank: number;
  createdAt: string;
  updatedAt: string;
};

export type EnvironmentsResult = {
  project: Project;
  environments: Environment[];
};

// getEnvironments returns a project's environments ordered by promotion rank.
// Open to project members.
export function getEnvironments(
  token: string,
  project: string,
): Promise<Result<EnvironmentsResult>> {
  return authGet<EnvironmentsResult>(token, `/api/v1/projects/${project}/environments`);
}

export type CreateEnvironmentInput = {
  slug: string;
  name: string;
  description: string;
  rank: number;
};

// createEnvironment defines a new environment under a project (project admins
// only).
export function createEnvironment(
  token: string,
  project: string,
  input: CreateEnvironmentInput,
): Promise<DataResult<Environment>> {
  return postData(token, `/api/v1/projects/${project}/environments`, input);
}

export type UpdateEnvironmentInput = {
  name: string;
  description: string;
  rank: number;
};

// updateEnvironment changes an environment's display fields and rank (project
// admins only). The slug is immutable.
export function updateEnvironment(
  token: string,
  project: string,
  env: string,
  input: UpdateEnvironmentInput,
): Promise<DataResult<Environment>> {
  return sendData(token, "PATCH", `/api/v1/projects/${project}/environments/${env}`, input);
}

// deleteEnvironment removes an environment (project admins only).
export function deleteEnvironment(
  token: string,
  project: string,
  env: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/projects/${project}/environments/${env}`);
}

// ---- my projects ----------------------------------------------------------

// MyProject is a project the signed-in user belongs to, annotated with their
// role so the project switcher and dashboard can show membership context.
export type MyProject = Project & { role: string };

// getMyProjects returns every project the signed-in user belongs to.
export async function getMyProjects(token: string): Promise<MyProject[]> {
  const res = await authGet<{ projects?: MyProject[] }>(
    token,
    "/api/v1/me/projects",
  );
  return res.ok ? (res.data.projects ?? []) : [];
}

// ---- profile ---------------------------------------------------------------

// updateProfile saves the signed-in user's editable profile fields.
export function updateProfile(
  token: string,
  input: { displayName: string },
): Promise<DataResult<User>> {
  return sendData<User>(token, "PATCH", "/api/v1/auth/me", input);
}

// deleteMyAccount permanently purges the signed-in user's account (GDPR erasure).
export async function deleteMyAccount(
  token: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/auth/me`, {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return {
        ok: false,
        error: data?.message || `Request failed (${res.status}).`,
      };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// updateEmail changes the signed-in user's email address.
export function updateEmail(
  token: string,
  email: string,
): Promise<DataResult<User>> {
  return sendData<User>(token, "PATCH", "/api/v1/auth/me/email", { email });
}

// changePassword verifies the user's current password then replaces it.
export async function changePassword(
  token: string,
  currentPassword: string,
  newPassword: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/auth/me/password`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ currentPassword, newPassword }),
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return {
        ok: false,
        error: data?.message || `Request failed (${res.status}).`,
      };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// postNoContent issues an authenticated POST for endpoints that return no body
// (204), mapping transport and HTTP errors into a simple ok/error result.
async function postNoContent(
  token: string,
  path: string,
  body: unknown,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return {
        ok: false,
        error: data?.message || `Request failed (${res.status}).`,
      };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

// ---- pipelines (CI) --------------------------------------------------------

// PipelineRunStatus is the lifecycle state of a run, job, or step.
export type PipelineRunStatus =
  | "pending"
  | "running"
  | "success"
  | "failure"
  | "cancelled"
  | "skipped";

// PipelineRun is one execution of a workflow.
export type PipelineRun = {
  id: string;
  runNumber: number;
  workflowPath?: string;
  status: PipelineRunStatus;
  event: string;
  ref: string;
  commitSha: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt: string;
};

// PipelineSummary is a workflow file plus its most recent run.
export type PipelineSummary = {
  workflowPath: string;
  name: string;
  lastRun?: PipelineRun;
};

// PipelineStep is a single step within a job, including its captured logs.
export type PipelineStep = {
  name: string;
  type: "run" | "uses";
  status: PipelineRunStatus;
  logs: string;
  startedAt?: string;
  finishedAt?: string;
};

// PipelineJob is a job and its steps within a run.
export type PipelineJob = {
  key: string;
  name: string;
  runsOn: string;
  status: PipelineRunStatus;
  startedAt?: string;
  finishedAt?: string;
  steps: PipelineStep[];
};

// PipelineRunDetail is a run with its fully expanded job/step tree.
export type PipelineRunDetail = PipelineRun & { jobs: PipelineJob[] };

// pipelinesResult is the workflow listing payload.
export type PipelinesResult = {
  repository: Repo;
  pipelines: PipelineSummary[];
};

// getPipelines returns a repository's workflows with their latest run status.
export function getPipelines(
  token: string,
  project: string,
  repo: string,
): Promise<Result<PipelinesResult>> {
  return authGet<PipelinesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pipelines`,
  );
}

// runsResult is the run-listing payload.
export type RunsResult = { repository: Repo; runs: PipelineRun[] };

// getPipelineRuns returns a repository's most recent runs across all pipelines.
export function getPipelineRuns(
  token: string,
  project: string,
  repo: string,
): Promise<Result<RunsResult>> {
  return authGet<RunsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pipelines/runs`,
  );
}

// runDetailResult is the single-run payload with its job/step tree.
export type RunDetailResult = { repository: Repo; run: PipelineRunDetail };

// getPipelineRun returns a single run (by number) with its full job/step tree.
// workflow is the repo-relative workflow path the run belongs to.
export function getPipelineRun(
  token: string,
  project: string,
  repo: string,
  number: number,
  workflow: string,
): Promise<Result<RunDetailResult>> {
  return authGet<RunDetailResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pipelines/runs/${number}?workflow=${encodeURIComponent(workflow)}`,
  );
}

// triggerPipelineRun runs a workflow manually on the given ref (empty = default).
export function triggerPipelineRun(
  token: string,
  project: string,
  repo: string,
  input: { workflow: string; ref?: string },
): Promise<DataResult<{ run: PipelineRun }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/pipelines`, input);
}

// cancelPipelineRun cancels a queued or running pipeline run. Returns ok: false
// with a 409 message when the run is already finished.
export function cancelPipelineRun(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return postNoContent(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pipelines/runs/${number}/cancel`,
    {},
  );
}

// ---- issues ----------------------------------------------------------------

export type Issue = {
  number: number;
  title: string;
  body: string;
  state: "open" | "closed";
  author: UserRef | null;
  comments: number;
  labels: { name: string; color: string }[];
  createdAt: string;
  updatedAt: string;
};

export type IssueComment = {
  id: number;
  body: string;
  author: UserRef | null;
  createdAt: string;
};

export type IssuesResult = { issues: Issue[] };

export function listIssues(
  token: string,
  project: string,
  repo: string,
  state: "open" | "closed" | "all" = "open",
  page = 1,
): Promise<Result<IssuesResult>> {
  return authGet<IssuesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/issues?state=${state}&page=${page}`,
  );
}

export type IssueDetailResult = { issue: Issue; comments: IssueComment[] };

export function getIssue(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<IssueDetailResult>> {
  return authGet<IssueDetailResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/issues/${number}`,
  );
}

export function createIssue(
  token: string,
  project: string,
  repo: string,
  input: { title: string; body: string },
): Promise<DataResult<{ issue: Issue }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/issues`, input);
}

export function editIssue(
  token: string,
  project: string,
  repo: string,
  number: number,
  input: { state?: string; title?: string },
): Promise<DataResult<{ issue: Issue }>> {
  return sendData(
    token,
    "PATCH",
    `/api/v1/projects/${project}/repos/${repo}/issues/${number}`,
    input,
  );
}

export function createIssueComment(
  token: string,
  project: string,
  repo: string,
  number: number,
  body: string,
): Promise<DataResult<{ comment: IssueComment }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/issues/${number}/comments`,
    { body },
  );
}

// ---- admin users -----------------------------------------------------------

export function listAdminUsers(token: string): Promise<Result<{ users: User[] }>> {
  return authGet<{ users: User[] }>(token, "/api/v1/admin/users");
}

export function adminSetUserActive(
  token: string,
  username: string,
  active: boolean,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return sendNoContent(token, "PATCH", `/api/v1/admin/users/${encodeURIComponent(username)}/active`, { active });
}

export function adminResetPassword(
  token: string,
  username: string,
  newPassword: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return postNoContent(token, `/api/v1/admin/users/${encodeURIComponent(username)}/reset-password`, { newPassword });
}

async function sendNoContent(
  token: string,
  method: "PATCH" | "PUT",
  path: string,
  body: unknown,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as { message?: string } | null;
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}
