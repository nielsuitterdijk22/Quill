"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { logoutAction, setCurrentProjectAction } from "../lib/actions";
import type { MyProject, User } from "../lib/api";

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
  { href: "/projects", label: "Projects", icon: "▤" },
  { href: "/settings", label: "Settings", icon: "⚙" },
];

// ADMIN_NAV holds entries only platform admins see (tenant-wide governance).
const ADMIN_NAV: NavItem[] = [
  { href: "/admin/policies", label: "Admin", icon: "🛡" },
];

// isRepoScoped is true for any path inside a specific repository, i.e.
// /projects/{project}/repos/{repo} and everything beneath it (code/commits/
// branches/blob/tree/pulls/settings). These browse code, not project
// management, so the top-level "Repositories" entry should light up for them.
function isRepoScoped(pathname: string): boolean {
  return /^\/projects\/[^/]+\/repos\/[^/]+(\/|$)/.test(pathname);
}

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  // "Repositories" owns both the standalone /repositories listing and every
  // repo-scoped /projects/{project}/repos/{repo}/... page.
  if (href === "/repositories") {
    if (pathname === href || pathname.startsWith(href + "/")) return true;
    return isRepoScoped(pathname);
  }
  // "Projects" stays active only for genuine project-management pages
  // (/projects, /projects/new, /projects/{project} landing) — never while
  // browsing a repo.
  if (href === "/projects") {
    if (isRepoScoped(pathname)) return false;
    return pathname === href || pathname.startsWith(href + "/");
  }
  return pathname === href || pathname.startsWith(href + "/");
}

type RepoCtx = { project: string; repo: string; ref: string };

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

// Extract repo context from /projects/{project}/repos/{repo}/* paths. Returns
// null for project-management pages (/projects, /projects/new,
// /projects/{project}, /projects/{project}/repos/new) and non-repo routes. Refs
// can contain slashes (e.g. feature/login-page), so the commits route — which
// never carries a path — keeps its whole tail as the ref, while tree/blob fall
// back to the first segment (their tail mixes ref and file path, which we can't
// split here).
function parseRepoCtx(pathname: string): RepoCtx | null {
  const m = pathname.match(
    /^\/projects\/([^/]+)\/repos\/([^/]+)(?:\/(branches|tree|commits|blob|pulls|pipelines|settings)(?:\/(.+))?)?\/?$/,
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
    project: safeDecode(m[1]),
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
  const b = `/projects/${encodeURIComponent(ctx.project)}/repos/${encodeURIComponent(ctx.repo)}`;
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
  const b = `/projects/${encodeURIComponent(ctx.project)}/repos/${encodeURIComponent(ctx.repo)}`;
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

// ProjectSwitcher is the sidebar's current-project dropdown. Picking a project
// sets the `quill_current_project` cookie (via setCurrentProjectAction) so the
// cross-cutting views — Repositories, Pull requests, Pipelines — scope to it;
// it does not navigate. A custom button + menu (rather than a native <select>)
// keeps the chrome consistent across the family of tools.
function ProjectSwitcher({
  projects,
  currentProject,
}: {
  projects: MyProject[];
  currentProject: string | null;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const active = projects.find((p) => p.slug === currentProject);
  const label = active?.name ?? "Select project";

  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div className="project-switcher" ref={ref}>
      <button
        className="project-switcher-btn"
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        {label}
        <span className="project-switcher-caret">{open ? "▲" : "▼"}</span>
      </button>
      {open && (
        <div className="project-switcher-menu" role="listbox">
          <form action={setCurrentProjectAction}>
            {projects.map((p) => (
              <button
                key={p.id}
                type="submit"
                name="project"
                value={p.slug}
                role="option"
                aria-selected={p.slug === currentProject}
                className={`project-switcher-item${p.slug === currentProject ? " active" : ""}`}
                onClick={() => setOpen(false)}
              >
                {p.name}
              </button>
            ))}
          </form>
          <hr className="project-switcher-divider" />
          <Link
            href="/projects/new"
            className="project-switcher-item"
            onClick={() => setOpen(false)}
          >
            + New project
          </Link>
        </div>
      )}
    </div>
  );
}

export function Sidebar({
  user,
  projects,
  currentProject,
}: {
  user: User;
  projects: MyProject[];
  currentProject: string | null;
}) {
  const pathname = usePathname() || "/";
  const repoCtx = parseRepoCtx(pathname);
  const navItems = user.isAdmin ? [...NAV, ...ADMIN_NAV] : NAV;

  return (
    <aside className="side">
      <div className="brand">
        <span className="dot" /> Quill
      </div>

      <div className="project">
        <span className="who">Signed in as</span>
        <b>{user.displayName || user.username}</b>
        {projects.length > 0 && (
          <ProjectSwitcher projects={projects} currentProject={currentProject} />
        )}
      </div>

      <nav className="nav">
        {navItems.map((it) => (
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
            <Link href={`/projects/${encodeURIComponent(repoCtx.project)}`}>
              {repoCtx.project}
            </Link>
            {" / "}
            <Link
              href={`/projects/${encodeURIComponent(repoCtx.project)}/repos/${encodeURIComponent(repoCtx.repo)}`}
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
        <div className="copy">
          © 2026 Quill ·{" "}
          <a
            href="https://github.com/nielsuitterdijk22/quill"
            target="_blank"
            rel="noopener noreferrer"
            className="copy-link"
          >
            Apache 2.0
          </a>
        </div>
      </div>
    </aside>
  );
}
