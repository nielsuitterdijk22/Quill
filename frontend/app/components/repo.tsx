// Presentational building blocks for the repository code browser (PR 5). These
// are server components — no client state — that render data the pages fetched.

import Link from "next/link";

import type { Commit, ContentEntry } from "../lib/api";

// repoBase is the URL prefix for a repository's pages.
export function repoBase(project: string, repo: string): string {
  return `/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}`;
}

// cloneHttpUrl builds the public HTTPS git URL for a repository, used by the
// clone widget. owner/name fall back to the project/repo slugs when Forgejo hasn't
// reported them.
export function cloneHttpUrl(
  publicUrl: string | undefined,
  owner: string | undefined,
  name: string | undefined,
  project: string,
  repo: string,
): string {
  const base = (publicUrl ?? "http://localhost:3000").replace(/\/+$/, "");
  return `${base}/${owner ?? project}/${name ?? repo}.git`;
}

// encodeRef encodes a ref for a catch-all route segment: each slash-separated
// part is percent-encoded but the slashes are preserved so the route receives
// the ref as multiple path segments (e.g. feature/login-page → two segments).
export function encodeRef(ref: string): string {
  return ref.split("/").map(encodeURIComponent).join("/");
}

// splitRef resolves a catch-all slug into a (ref, path) pair. Branch names can
// contain slashes, so we pick the longest leading run of segments that matches a
// known ref; anything after it is the file path. Falls back to treating the
// first segment as the ref (covers commit SHAs and tags not in the list).
export function splitRef(
  slug: string[],
  refNames: string[],
): { ref: string; path: string } {
  const known = new Set(refNames);
  for (let i = slug.length; i >= 1; i--) {
    const candidate = slug.slice(0, i).join("/");
    if (known.has(candidate)) {
      return { ref: candidate, path: slug.slice(i).join("/") };
    }
  }
  return { ref: slug[0] ?? "", path: slug.slice(1).join("/") };
}

// treeHref links to a directory listing at a ref/path.
export function treeHref(
  project: string,
  repo: string,
  ref: string,
  path = "",
): string {
  const base = `${repoBase(project, repo)}/tree/${encodeRef(ref)}`;
  return path ? `${base}/${encodePath(path)}` : base;
}

// blobHref links to a file view at a ref/path.
export function blobHref(
  project: string,
  repo: string,
  ref: string,
  path: string,
): string {
  return `${repoBase(project, repo)}/blob/${encodeRef(ref)}/${encodePath(path)}`;
}

// commitsHref links to the commit log at a ref.
export function commitsHref(project: string, repo: string, ref: string): string {
  return `${repoBase(project, repo)}/commits/${encodeRef(ref)}`;
}

// encodePath encodes each path segment while keeping the slashes between them.
export function encodePath(path: string): string {
  return path.split("/").filter(Boolean).map(encodeURIComponent).join("/");
}

// humanBytes renders a byte count compactly (e.g. "1.2 KB").
export function humanBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

const VIS_CLASS: Record<string, string> = {
  public: "green",
  internal: "amber",
  private: "accent",
};

// VisibilityBadge renders a colored badge for a repo's visibility.
export function VisibilityBadge({ visibility }: { visibility: string }) {
  return (
    <span className={`badge ${VIS_CLASS[visibility] ?? ""}`}>{visibility}</span>
  );
}

export type RepoTab =
  | "code"
  | "commits"
  | "branches"
  | "pulls"
  | "pipelines"
  | "settings";

// RepoHeader renders the repo title, visibility, and the code/commits/branches/
// pull-requests/settings tab navigation shared across every repository page.
export function RepoHeader({
  project,
  repo,
  visibility,
  refName,
  active,
}: {
  project: string;
  repo: string;
  visibility: string;
  refName: string;
  active: RepoTab;
}) {
  const base = repoBase(project, repo);
  const tabs: { key: RepoTab; label: string; href: string }[] = [
    { key: "code", label: "Code", href: treeHref(project, repo, refName) },
    { key: "commits", label: "Commits", href: commitsHref(project, repo, refName) },
    { key: "branches", label: "Branches", href: `${base}/branches` },
    { key: "pulls", label: "Pull requests", href: `${base}/pulls` },
    { key: "pipelines", label: "Pipelines", href: `${base}/pipelines` },
    { key: "settings", label: "Settings", href: `${base}/settings` },
  ];
  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <Link href={`/projects/${encodeURIComponent(project)}`}>{project}</Link>{" "}
        <span>/</span> <span>{repo}</span>
      </div>
      <div className="top">
        <h1>
          {project}/<b>{repo}</b>
        </h1>
        <VisibilityBadge visibility={visibility} />
      </div>
      <nav className="rtabs">
        {tabs.map((t) => (
          <Link
            key={t.key}
            href={t.href}
            className={t.key === active ? "active" : ""}
          >
            {t.label}
          </Link>
        ))}
      </nav>
    </>
  );
}

// PathBreadcrumb renders a clickable path within the code browser at a ref.
export function PathBreadcrumb({
  project,
  repo,
  refName,
  path,
}: {
  project: string;
  repo: string;
  refName: string;
  path: string;
}) {
  const parts = path.split("/").filter(Boolean);
  let acc = "";
  return (
    <div className="path-crumbs">
      <Link href={treeHref(project, repo, refName)}>{repo}</Link>
      {parts.map((part, i) => {
        acc = acc ? `${acc}/${part}` : part;
        const last = i === parts.length - 1;
        return (
          <span key={acc}>
            <span className="sep">/</span>
            {last ? (
              <span className="cur">{part}</span>
            ) : (
              <Link href={treeHref(project, repo, refName, acc)}>{part}</Link>
            )}
          </span>
        );
      })}
    </div>
  );
}

// CodeView renders file content with a line-number gutter.
export function CodeView({ content }: { content: string }) {
  const lines = content.replace(/\n$/, "").split("\n");
  const gutter = lines.map((_, i) => i + 1).join("\n");
  return (
    <div className="codeview">
      <pre className="gutter" aria-hidden="true">
        {gutter}
      </pre>
      <pre className="code">{content.replace(/\n$/, "")}</pre>
    </div>
  );
}

// ReadmeView renders a repository README below the file tree. When rendered HTML
// is available (markdown via Forgejo's markup engine) it is shown as formatted
// markup; otherwise the raw text is shown in a monospace block.
export function ReadmeView({
  name,
  html,
  raw,
}: {
  name: string;
  html: string | null;
  raw: string;
}) {
  return (
    <div className="panel readme">
      <h2>
        <span className="fn">{name}</span>
      </h2>
      {html ? (
        <div
          className="readme-body markdown-body"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <div className="readme-body">
          <pre>{raw}</pre>
        </div>
      )}
    </div>
  );
}

// shortSha trims a commit SHA for display.
export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

// BrowseError renders a non-404 browse failure (403 no-access, 502 git backend
// unavailable, or an unreachable backend) with repository breadcrumbs.
export function BrowseError({
  project,
  repo,
  status,
  message,
}: {
  project: string;
  repo: string;
  status: number;
  message: string;
}) {
  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <Link href={`/projects/${encodeURIComponent(project)}`}>{project}</Link>{" "}
        <span>/</span> <span>{repo}</span>
      </div>
      <h1>
        {project}/{repo}
      </h1>
      <div className="banner">
        {status === 403
          ? "You are not a member of this project."
          : status === 502
            ? "The git backend is unavailable for this repository."
            : message}
      </div>
    </>
  );
}

// firstLine returns the summary (first line) of a commit message.
export function firstLine(message: string): string {
  const nl = message.indexOf("\n");
  return nl === -1 ? message : message.slice(0, nl);
}

// fmtDate renders an ISO timestamp as a short, locale-independent date.
export function fmtDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const months = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
  ];
  return `${months[d.getUTCMonth()]} ${d.getUTCDate()}, ${d.getUTCFullYear()}`;
}

// DirView renders a directory listing as a file tree. When path is non-empty a
// ".." row links to the parent. An optional latest commit renders as a strip
// attached to the top of the panel.
export function DirView({
  project,
  repo,
  refName,
  path,
  entries,
  latest,
}: {
  project: string;
  repo: string;
  refName: string;
  path: string;
  entries: ContentEntry[];
  latest?: Commit | null;
}) {
  const showUp = path !== "";
  const parent = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
  return (
    <>
      {latest && (
        <div className="commit-strip">
          <span className="mono">{shortSha(latest.sha)}</span>
          <span className="msg">{firstLine(latest.message)}</span>
          <span className="sha">{latest.authorLogin || latest.authorName}</span>
        </div>
      )}
      <div className={`panel ${latest ? "attached" : ""}`}>
        {showUp && (
          <Link
            className="row-item"
            href={treeHref(project, repo, refName, parent)}
          >
            <span className="tree-icon dir">⤴</span>
            <span className="nm">..</span>
          </Link>
        )}
        {entries.length === 0 && !showUp ? (
          <div className="empty">This directory is empty.</div>
        ) : (
          entries.map((e) =>
            e.type === "dir" ? (
              <Link
                className="row-item"
                key={e.path}
                href={treeHref(project, repo, refName, e.path)}
              >
                <span className="tree-icon dir">▸</span>
                <span className="nm">{e.name}</span>
              </Link>
            ) : (
              <Link
                className="row-item"
                key={e.path}
                href={blobHref(project, repo, refName, e.path)}
              >
                <span className="tree-icon">▤</span>
                <span className="nm">{e.name}</span>
                <span className="spacer" />
                <span className="sub">{humanBytes(e.size)}</span>
              </Link>
            ),
          )
        )}
      </div>
    </>
  );
}
