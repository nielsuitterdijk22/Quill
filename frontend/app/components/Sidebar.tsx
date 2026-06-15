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
  { href: "/orgs", label: "Organizations", icon: "▤" },
  { href: "/pulls", label: "Pull requests", icon: "⤭", soon: true },
  { href: "/pipelines", label: "Pipelines", icon: "▷", soon: true },
  { href: "/teams", label: "Teams", icon: "◎", soon: true },
  { href: "/settings", label: "Settings", icon: "⚙", soon: true },
];

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  return pathname === href || pathname.startsWith(href + "/");
}

type RepoCtx = { org: string; repo: string; ref: string };

// Extract repo context from /orgs/{org}/{repo}/* paths. Returns null for
// top-level org pages (/orgs/{org}/new) and non-repo routes.
function parseRepoCtx(pathname: string): RepoCtx | null {
  const m = pathname.match(
    /^\/orgs\/([^/]+)\/([^/]+)(?:\/(tree|commits|blob)\/([^/]+))?/,
  );
  if (!m || !m[2] || m[2] === "new") return null;
  return {
    org: decodeURIComponent(m[1]),
    repo: decodeURIComponent(m[2]),
    // Use the ref extracted from the URL path when available so the Commits
    // link stays consistent while browsing; fall back to "main".
    ref: m[4] ? decodeURIComponent(m[4]) : "main",
  };
}

const REPO_TABS = [
  { key: "code",    label: "Code",          icon: "▤" },
  { key: "commits", label: "Commits",        icon: "◷" },
  { key: "branches",label: "Branches",       icon: "⎇" },
  { key: "pulls",   label: "Pull requests",  icon: "⤭" },
  { key: "settings",label: "Settings",       icon: "⚙" },
] as const;

type RepoTabKey = (typeof REPO_TABS)[number]["key"];

function repoTabHref(ctx: RepoCtx, key: RepoTabKey): string {
  const b = `/orgs/${encodeURIComponent(ctx.org)}/${encodeURIComponent(ctx.repo)}`;
  switch (key) {
    case "code":     return b;
    case "commits":  return `${b}/commits/${encodeURIComponent(ctx.ref)}`;
    case "branches": return `${b}/branches`;
    case "pulls":    return `${b}/pulls`;
    case "settings": return `${b}/settings`;
  }
}

function repoTabActive(pathname: string, key: RepoTabKey, ctx: RepoCtx): boolean {
  const b = `/orgs/${encodeURIComponent(ctx.org)}/${encodeURIComponent(ctx.repo)}`;
  switch (key) {
    case "code":     return pathname === b || pathname.startsWith(`${b}/tree/`) || pathname.startsWith(`${b}/blob/`);
    case "commits":  return pathname.startsWith(`${b}/commits/`);
    case "branches": return pathname === `${b}/branches`;
    case "pulls":    return pathname.startsWith(`${b}/pulls`);
    case "settings": return pathname === `${b}/settings`;
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
              href={`/orgs/${encodeURIComponent(repoCtx.org)}/${encodeURIComponent(repoCtx.repo)}`}
            >
              <b>{repoCtx.repo}</b>
            </Link>
          </div>
          <nav className="repo-ctx-nav">
            {REPO_TABS.map((t) => (
              <Link
                key={t.key}
                href={repoTabHref(repoCtx, t.key)}
                className={repoTabActive(pathname, t.key, repoCtx) ? "active" : ""}
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
