"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { logoutAction } from "../lib/actions";
import type { User } from "../lib/api";

export type NavItem = {
  href: string;
  label: string;
  icon: string;
  soon?: boolean;
};

const NAV: NavItem[] = [
  { href: "/", label: "Dashboard", icon: "◧" },
  { href: "/repositories", label: "Repositories", icon: "⎇" },
  { href: "/pulls", label: "Pull requests", icon: "⤭" },
  { href: "/pipelines", label: "Pipelines", icon: "▷" },
  { href: "/teams", label: "Teams", icon: "◎" },
  { href: "/orgs", label: "Organizations", icon: "▤" },
  { href: "/settings", label: "Settings", icon: "⚙" },
];

// isRepoScoped is true for any path inside a specific repository, i.e.
// /orgs/{org}/repos/{repo} and everything beneath it (code/commits/branches/
// blob/tree/pulls/settings). These browse code, not org management, so the
// top-level "Repositories" entry should light up for them.
function isRepoScoped(pathname: string): boolean {
  return /^\/orgs\/[^/]+\/repos\/[^/]+(\/|$)/.test(pathname);
}

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  // "Repositories" owns both the standalone /repositories listing and every
  // repo-scoped /orgs/{org}/repos/{repo}/... page.
  if (href === "/repositories") {
    if (pathname === href || pathname.startsWith(href + "/")) return true;
    return isRepoScoped(pathname);
  }
  // "Organizations" stays active only for genuine org-management pages
  // (/orgs, /orgs/new, /orgs/{org} landing) — never while browsing a repo.
  if (href === "/orgs") {
    if (isRepoScoped(pathname)) return false;
    return pathname === href || pathname.startsWith(href + "/");
  }
  return pathname === href || pathname.startsWith(href + "/");
}

type RepoCtx = { org: string; repo: string; ref: string };

// safeDecode decodes a URL path segment, returning it unchanged if it isn't
// valid percent-encoding. decodeURIComponent throws on malformed input (e.g. a
// stray "%"), which would otherwise crash the whole app shell on a bad URL.
function safeDecode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

// Extract repo context from /orgs/{org}/repos/{repo}/* paths. Returns null for
// org-management pages (/orgs, /orgs/new, /orgs/{org}, /orgs/{org}/repos/new)
// and non-repo routes. Refs can contain slashes (e.g. feature/login-page), so
// the commits route — which never carries a path — keeps its whole tail as the
// ref, while tree/blob fall back to the first segment (their tail mixes ref and
// file path, which we can't split here).
function parseRepoCtx(pathname: string): RepoCtx | null {
  const m = pathname.match(
    /^\/orgs\/([^/]+)\/repos\/([^/]+)(?:\/(tree|commits|blob)\/(.+?))?\/?$/,
  );
  if (!m || !m[2] || m[2] === "new") return null;
  let ref = "main";
  if (m[4]) {
    if (m[3] === "commits") {
      ref = m[4].split("/").map(safeDecode).join("/");
    } else {
      ref = safeDecode(m[4].split("/")[0]);
    }
  }
  return {
    org: safeDecode(m[1]),
    repo: safeDecode(m[2]),
    ref,
  };
}

const REPO_TABS = [
  { key: "code", label: "Code", icon: "▤" },
  { key: "commits", label: "Commits", icon: "◷" },
  { key: "branches", label: "Branches", icon: "⎇" },
  { key: "pulls", label: "Pull requests", icon: "⤭" },
  { key: "pipelines", label: "Pipelines", icon: "▷" },
  { key: "settings", label: "Settings", icon: "⚙" },
] as const;

type RepoTabKey = (typeof REPO_TABS)[number]["key"];

function repoTabHref(ctx: RepoCtx, key: RepoTabKey): string {
  const b = `/orgs/${encodeURIComponent(ctx.org)}/repos/${encodeURIComponent(ctx.repo)}`;
  switch (key) {
    case "code":
      return b;
    case "commits":
      return `${b}/commits/${ctx.ref.split("/").map(encodeURIComponent).join("/")}`;
    case "branches":
      return `${b}/branches`;
    case "pulls":
      return `${b}/pulls`;
    case "pipelines":
      return `${b}/pipelines`;
    case "settings":
      return `${b}/settings`;
  }
}

function repoTabActive(
  pathname: string,
  key: RepoTabKey,
  ctx: RepoCtx,
): boolean {
  const b = `/orgs/${encodeURIComponent(ctx.org)}/repos/${encodeURIComponent(ctx.repo)}`;
  switch (key) {
    case "code":
      return (
        pathname === b ||
        pathname.startsWith(`${b}/tree/`) ||
        pathname.startsWith(`${b}/blob/`)
      );
    case "commits":
      return pathname.startsWith(`${b}/commits/`);
    case "branches":
      return pathname === `${b}/branches`;
    case "pulls":
      return pathname.startsWith(`${b}/pulls`);
    case "pipelines":
      return pathname.startsWith(`${b}/pipelines`);
    case "settings":
      return pathname === `${b}/settings`;
  }
}

export function Sidebar({ user }: { user: User }) {
  const pathname = usePathname() || "/";
  const repoCtx = parseRepoCtx(pathname);

  return (
    <aside className="side">
      <div className="brand">
        <span className="dot" /> Quill
      </div>

      <div className="org">
        <span className="who">Signed in as</span>
        <b>{user.displayName || user.username}</b>
      </div>

      <nav className="nav">
        {NAV.map((it) => (
          <Link
            key={it.href}
            href={it.href}
            className={isActive(pathname, it.href) ? "active" : ""}
          >
            <span className="ic">{it.icon}</span>
            {it.label}
            {it.soon && <span className="tag">soon</span>}
          </Link>
        ))}
      </nav>

      {repoCtx && (
        <div className="repo-ctx">
          <div className="repo-ctx-label">
            <Link href={`/orgs/${encodeURIComponent(repoCtx.org)}`}>
              {repoCtx.org}
            </Link>
            {" / "}
            <Link
              href={`/orgs/${encodeURIComponent(repoCtx.org)}/repos/${encodeURIComponent(repoCtx.repo)}`}
            >
              <b>{repoCtx.repo}</b>
            </Link>
          </div>
          <nav className="repo-ctx-nav">
            {REPO_TABS.map((t) => (
              <Link
                key={t.key}
                href={repoTabHref(repoCtx, t.key)}
                className={
                  repoTabActive(pathname, t.key, repoCtx) ? "active" : ""
                }
              >
                <span className="ic">{t.icon}</span>
                {t.label}
              </Link>
            ))}
          </nav>
        </div>
      )}

      <div className="foot">
        <form action={logoutAction}>
          <button className="logout-btn" type="submit">
            Sign out
          </button>
        </form>
        <div className="copy">© 2026 Quill</div>
      </div>
    </aside>
  );
}
