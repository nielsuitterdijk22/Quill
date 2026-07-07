// Shared types for the Quill API.

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
  isPersonal: boolean;
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

// EntryCommit is the last commit that touched a directory entry, attached to
// each ContentEntry so the tree view can show when a file was last edited.
export type EntryCommit = {
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
  lastCommit?: EntryCommit;
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

export type PolicyDenial = {
  scope: string;
  selector: string;
  message: string;
};

export type PolicyScope = "repo" | "project" | "tenant";

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

export type GitCredential = { id: string; username: string; token: string };
export type GitTokenSummary = { id: string; name: string; createdAt: string };

export type SSHKey = {
  id: number;
  title: string;
  key: string;
  fingerprint: string;
};

export type Result<T> =
  | { ok: true; data: T }
  | { ok: false; status: number; message: string };

export type DataResult<T> = { ok: true; data: T } | { ok: false; error: string };

export type MutationResult =
  | { ok: true; slug: string }
  | { ok: false; error: string };

export type ReposResult = { project: Project; repositories: Repo[] };

export type BranchesResult = {
  repository: Repo;
  defaultBranch: string;
  branches: Branch[];
};

export type CommitsResult = { repository: Repo; commits: Commit[] };

export type CommitDetailResult = {
  repository: Repo;
  commit: Commit;
  files: DiffFile[];
};

export type ContentsResult = { repository: Repo; contents: Contents };

export type UpdateRepoInput = {
  name?: string;
  description?: string;
  visibility?: string;
  defaultBranch?: string;
  slug?: string;
  archived?: boolean;
};

export type PullsResult = { repository: Repo; pulls: PullRequest[] };

export type RepoPull = {
  projectSlug: string;
  repoSlug: string;
  repoName: string;
  pull: PullRequest;
};

export type MyPullsResult = { pulls: RepoPull[] };

export type PullResult = { repository: Repo; pull: PullRequest };

export type DiffResult = { files: DiffFile[] };

export type CommentsResult = { comments: PullComment[] };

export type ReviewsResult = { reviews: Review[]; gate: PolicyGate };

export type LineComment = {
  id: number;
  path: string;
  line: number;
  body: string;
  author?: string;
  createdAt: string;
};

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

export type PoliciesResult = {
  repository: Repo;
  policies: BranchPolicy[];
  inherited: BranchPolicy[];
};

export type ProjectPoliciesResult = {
  project: Project;
  policies: BranchPolicy[];
  inherited: BranchPolicy[];
};

export type TenantPoliciesResult = {
  tenant: { slug: string; name: string };
  policies: BranchPolicy[];
};

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

export type EnvironmentPoliciesResult = {
  repository: Repo;
  policies: EnvironmentPolicy[];
  inherited: EnvironmentPolicy[];
};

export type ProjectEnvironmentPoliciesResult = {
  project: Project;
  policies: EnvironmentPolicy[];
  inherited: EnvironmentPolicy[];
};

export type TenantEnvironmentPoliciesResult = {
  tenant: { slug: string; name: string };
  policies: EnvironmentPolicy[];
};

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

export type CreateEnvironmentInput = {
  slug: string;
  name: string;
  description: string;
  rank: number;
};

export type UpdateEnvironmentInput = {
  name: string;
  description: string;
  rank: number;
};

export type MyProject = Project & { role: string };

export type PipelineRunStatus =
  | "pending"
  | "running"
  | "success"
  | "failure"
  | "cancelled"
  | "skipped";

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

export type PipelineSummary = {
  workflowPath: string;
  name: string;
  lastRun?: PipelineRun;
};

export type PipelineStep = {
  name: string;
  type: "run" | "uses";
  status: PipelineRunStatus;
  logs: string;
  startedAt?: string;
  finishedAt?: string;
};

export type PipelineJob = {
  key: string;
  name: string;
  runsOn: string;
  status: PipelineRunStatus;
  startedAt?: string;
  finishedAt?: string;
  steps: PipelineStep[];
};

export type PipelineRunDetail = PipelineRun & { jobs: PipelineJob[] };

export type PipelinesResult = {
  repository: Repo;
  pipelines: PipelineSummary[];
};

export type RunsResult = { repository: Repo; runs: PipelineRun[] };

export type RunDetailResult = { repository: Repo; run: PipelineRunDetail };

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

export type IssueDetailResult = { issue: Issue; comments: IssueComment[] };
