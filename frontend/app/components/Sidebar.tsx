"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, useTransition } from "react";

import { useClerk, useOrganization } from "@clerk/nextjs";
import { useRouter } from "next/navigation";

import { setCurrentProjectAction } from "../lib/actions";
import type { MyProject, User } from "../lib/api";
import { ThemeToggle } from "./ThemeToggle";

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
  { href: "/admin/policies", label: "Policies", icon: "🛡" },
  { href: "/admin/users", label: "Users", icon: "◈" },
  { href: "/admin/audit-log", label: "Audit log", icon: "◎" },
];

// Top-level paths that are app routes, not owner namespaces.
const RESERVED_TOP_LEVEL = new Set([
  "projects", "settings", "admin", "repositories", "pulls", "pipelines",
  "sign-in", "sign-up", "login", "register", "api",
]);

// isRepoScoped is true for any path inside a specific repository. Handles both
// the long form /projects/{project}/repos/{repo}/... and the short namespace
// form /{owner}/{repo}/...
function isRepoScoped(pathname: string): boolean {
  if (/^\/projects\/[^/]+\/repos\/[^/]+(\/|$)/.test(pathname)) return true;
  const m = pathname.match(/^\/([^/]+)\/[^/]+(\/|$)/);
  if (!m) return false;
  return !RESERVED_TOP_LEVEL.has(safeDecode(m[1]));
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

// Extract repo context from both URL forms:
//   long:  /projects/{project}/repos/{repo}/[tab]/[ref/path...]
//   short: /{owner}/{repo}/[tab]/[ref/path...]
// Returns null for project management pages and non-repo routes.
function parseRepoCtx(pathname: string): RepoCtx | null {
  // Long form first.
  const longM = pathname.match(
    /^\/projects\/([^/]+)\/repos\/([^/]+)(?:\/(branches|tree|commits|blob|issues|pulls|pipelines|settings)(?:\/(.+))?)?\/?$/,
  );
  if (longM && longM[2] && longM[2] !== "new") {
    let ref = "main";
    if (longM[4]) {
      ref = longM[3] === "commits"
        ? longM[4].split("/").map(safeDecode).join("/")
        : safeDecode(longM[4].split("/")[0]);
    }
    return { project: safeDecode(longM[1]), repo: safeDecode(longM[2]), ref };
  }

  // Short namespace form /{owner}/{repo}/...
  const shortM = pathname.match(
    /^\/([^/]+)\/([^/]+)(?:\/(branches|tree|commits|blob|issues|pulls|pipelines|settings)(?:\/(.+))?)?\/?$/,
  );
  if (!shortM || !shortM[2]) return null;
  const owner = safeDecode(shortM[1]);
  const repo = safeDecode(shortM[2]);
  if (RESERVED_TOP_LEVEL.has(owner) || repo === "new") return null;
  let ref = "main";
  if (shortM[4]) {
    ref = shortM[3] === "commits"
      ? shortM[4].split("/").map(safeDecode).join("/")
      : safeDecode(shortM[4].split("/")[0]);
  }
  return { project: owner, repo, ref };
}

const REPO_TABS = [
  { key: "code", label: "Code", icon: "▤" },
  { key: "commits", label: "Commits", icon: "◷" },
  { key: "branches", label: "Branches", icon: "⎇" },
  { key: "issues", label: "Issues", icon: "○" },
  { key: "pulls", label: "Pull requests", icon: "⤭" },
  { key: "pipelines", label: "Pipelines", icon: "▷" },
  { key: "settings", label: "Settings", icon: "⚙" },
] as const;

type RepoTabKey = (typeof REPO_TABS)[number]["key"];

function repoTabHref(ctx: RepoCtx, key: RepoTabKey): string {
  const b = `/${encodeURIComponent(ctx.project)}/${encodeURIComponent(ctx.repo)}`;
  switch (key) {
    case "code":
      return b;
    case "commits":
      return `${b}/commits/${ctx.ref.split("/").map(encodeURIComponent).join("/")}`;
    case "branches":
      return `${b}/branches`;
    case "issues":
      return `${b}/issues`;
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
  // Accept both short /{project}/{repo}/... and long /projects/{project}/repos/{repo}/...
  const shortB = `/${encodeURIComponent(ctx.project)}/${encodeURIComponent(ctx.repo)}`;
  const longB = `/projects/${encodeURIComponent(ctx.project)}/repos/${encodeURIComponent(ctx.repo)}`;
  function check(b: string): boolean {
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
      case "issues":
        return pathname.startsWith(`${b}/issues`);
      case "pulls":
        return pathname.startsWith(`${b}/pulls`);
      case "pipelines":
        return pathname.startsWith(`${b}/pipelines`);
      case "settings":
        return pathname === `${b}/settings`;
    }
  }
  return check(shortB) || check(longB);
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
  const [pending, startTransition] = useTransition();
  const ref = useRef<HTMLDivElement>(null);
  const router = useRouter();
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

  function selectProject(slug: string) {
    setOpen(false);
    startTransition(async () => {
      const data = new FormData();
      data.set("project", slug);
      await setCurrentProjectAction(data);
      router.refresh();
    });
  }

  return (
    <div className="project-switcher" ref={ref}>
      <button
        className="project-switcher-btn"
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={pending}
      >
        {label}
        <span className="project-switcher-caret">{open ? "▲" : "▼"}</span>
      </button>
      {open && (
        <div className="project-switcher-menu" role="listbox">
          {projects.map((p) => (
            <button
              key={p.id}
              type="button"
              role="option"
              aria-selected={p.slug === currentProject}
              className={`project-switcher-item${p.slug === currentProject ? " active" : ""}`}
              onClick={() => selectProject(p.slug)}
            >
              {p.name}
            </button>
          ))}
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
  const { signOut } = useClerk();
  const { organization } = useOrganization();
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
        {organization && <span className="who">{organization.name}</span>}
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
            <Link href={`/${encodeURIComponent(repoCtx.project)}`}>
              {repoCtx.project}
            </Link>
            {" / "}
            <Link
              href={`/${encodeURIComponent(repoCtx.project)}/${encodeURIComponent(repoCtx.repo)}`}
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
        <ThemeToggle />
        <button
          className="logout-btn"
          onClick={() => signOut({ redirectUrl: "/sign-in" })}
        >
          Sign out
        </button>
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
