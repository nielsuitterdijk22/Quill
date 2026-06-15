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

export type Org = {
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
};

// BranchPolicy is a repository branch-protection rule owned by Quill.
export type BranchPolicy = {
  pattern: string;
  requiredApprovals: number;
  dismissStaleApprovals: boolean;
  requireUpToDate: boolean;
  blockForcePush: boolean;
  requirePullRequest: boolean;
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

// listOrgs returns all organizations visible to the authenticated user, or an
// empty list when the backend is unreachable so pages can render a degraded state.
export async function listOrgs(token: string): Promise<Org[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/orgs`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { organizations?: Org[] };
    return data.organizations ?? [];
  } catch {
    return [];
  }
}

// listReposByOrg returns the repositories within an org, or an empty list on error.
export async function listReposByOrg(
  token: string,
  slug: string,
): Promise<Repo[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/orgs/${slug}/repos`, {
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

// getOrg fetches a single organization.
export function getOrg(token: string, slug: string): Promise<Result<Org>> {
  return authGet<Org>(token, `/api/v1/orgs/${slug}`);
}

// reposResult is the org-detail repository listing payload.
export type ReposResult = { organization: Org; repositories: Repo[] };

// getReposByOrg returns an org plus its repositories, preserving HTTP status.
export function getReposByOrg(
  token: string,
  slug: string,
): Promise<Result<ReposResult>> {
  return authGet<ReposResult>(token, `/api/v1/orgs/${slug}/repos`);
}

// getRepo fetches a single repository's metadata.
export function getRepo(
  token: string,
  org: string,
  repo: string,
): Promise<Result<Repo>> {
  return authGet<Repo>(token, `/api/v1/orgs/${org}/repos/${repo}`);
}

// branchesResult is the branch listing payload.
export type BranchesResult = {
  repository: Repo;
  defaultBranch: string;
  branches: Branch[];
};

export function getBranches(
  token: string,
  org: string,
  repo: string,
): Promise<Result<BranchesResult>> {
  return authGet<BranchesResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/branches`,
  );
}

// commitsResult is the commit log payload.
export type CommitsResult = { repository: Repo; commits: Commit[] };

export function getCommits(
  token: string,
  org: string,
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
    `/api/v1/orgs/${org}/repos/${repo}/commits?${q.toString()}`,
  );
}

// contentsResult is the directory/file contents payload.
export type ContentsResult = { repository: Repo; contents: Contents };

export function getContents(
  token: string,
  org: string,
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
    `/api/v1/orgs/${org}/repos/${repo}/contents${qs ? `?${qs}` : ""}`,
  );
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

// createOrg provisions an organization (and its Forgejo mirror).
export function createOrg(
  token: string,
  input: { slug: string; name: string; description: string },
): Promise<MutationResult> {
  return postCreate(token, "/api/v1/orgs", input);
}

// createRepo provisions a repository under an org (and its Forgejo mirror).
export function createRepo(
  token: string,
  org: string,
  input: {
    slug: string;
    name: string;
    description: string;
    visibility: string;
  },
): Promise<MutationResult> {
  return postCreate(token, `/api/v1/orgs/${org}/repos`, input);
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

// updateRepo changes a repository's general settings (org owners / admins only)
// and returns the repository's new state.
export function updateRepo(
  token: string,
  org: string,
  repo: string,
  input: UpdateRepoInput,
): Promise<DataResult<Repo>> {
  return sendData<Repo>(token, "PATCH", `/api/v1/orgs/${org}/repos/${repo}`, input);
}

// deleteRepo permanently removes a repository (org owners / admins only).
export function deleteRepo(
  token: string,
  org: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/orgs/${org}/repos/${repo}`);
}

// ---- pull requests ---------------------------------------------------------

// pullsResult is the PR listing payload.
export type PullsResult = { repository: Repo; pulls: PullRequest[] };

// getPulls returns a repository's pull requests filtered by state.
export function getPulls(
  token: string,
  org: string,
  repo: string,
  state: "open" | "closed" | "all" = "open",
): Promise<Result<PullsResult>> {
  return authGet<PullsResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls?state=${state}`,
  );
}

// pullResult is the single-PR payload.
export type PullResult = { repository: Repo; pull: PullRequest };

export function getPull(
  token: string,
  org: string,
  repo: string,
  number: number,
): Promise<Result<PullResult>> {
  return authGet<PullResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}`,
  );
}

// diffResult is a PR's parsed diff payload.
export type DiffResult = { files: DiffFile[] };

export function getPullDiff(
  token: string,
  org: string,
  repo: string,
  number: number,
): Promise<Result<DiffResult>> {
  return authGet<DiffResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}/diff`,
  );
}

// commentsResult is a PR's conversation payload.
export type CommentsResult = { comments: PullComment[] };

export function getPullComments(
  token: string,
  org: string,
  repo: string,
  number: number,
): Promise<Result<CommentsResult>> {
  return authGet<CommentsResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}/comments`,
  );
}

// reviewsResult carries a PR's reviews and the policy gate for its base branch.
export type ReviewsResult = { reviews: Review[]; gate: PolicyGate };

export function getPullReviews(
  token: string,
  org: string,
  repo: string,
  number: number,
): Promise<Result<ReviewsResult>> {
  return authGet<ReviewsResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}/reviews`,
  );
}

// policiesResult is the branch-policy listing payload.
export type PoliciesResult = { repository: Repo; policies: BranchPolicy[] };

export function getBranchPolicies(
  token: string,
  org: string,
  repo: string,
): Promise<Result<PoliciesResult>> {
  return authGet<PoliciesResult>(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/policies`,
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
  org: string,
  repo: string,
  input: { title: string; body: string; head: string; base: string },
): Promise<DataResult<{ pull: PullRequest }>> {
  return postData(token, `/api/v1/orgs/${org}/repos/${repo}/pulls`, input);
}

// createPullComment adds a comment to a pull request's conversation.
export function createPullComment(
  token: string,
  org: string,
  repo: string,
  number: number,
  body: string,
): Promise<DataResult<{ comment: PullComment }>> {
  return postData(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}/comments`,
    { body },
  );
}

// mergePull merges a pull request using the given method.
export function mergePull(
  token: string,
  org: string,
  repo: string,
  number: number,
  method: "merge" | "squash" | "rebase",
): Promise<DataResult<{ pull: PullRequest }>> {
  return postData(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}/merge`,
    { method },
  );
}

// createPullReview submits a review (approve, request changes, or comment).
export function createPullReview(
  token: string,
  org: string,
  repo: string,
  number: number,
  input: { event: ReviewState; body: string },
): Promise<DataResult<{ review: Review }>> {
  return postData(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/pulls/${number}/reviews`,
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
};

// setBranchPolicy creates or updates a branch policy (org owners / admins only).
export function setBranchPolicy(
  token: string,
  org: string,
  repo: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/orgs/${org}/repos/${repo}/policies`, input);
}

// deleteBranchPolicy removes the policy for a branch pattern.
export function deleteBranchPolicy(
  token: string,
  org: string,
  repo: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/orgs/${org}/repos/${repo}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}
