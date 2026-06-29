import Link from "next/link";

import {
  getMeta,
  getMyProjects,
  getMyContributions,
  getMyPulls,
  listReposByProject,
  type Repo,
  type MyProject,
} from "../lib/api";
import { getProfileAvatar, getToken, requireSession } from "../lib/session";
import { ContributionGraph } from "../components/ContributionGraph";
import { VisibilityBadge } from "../components/repo";

function joinedLabel(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, { month: "long", year: "numeric" });
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  return (parts[0][0] + (parts[1]?.[0] ?? "")).toUpperCase();
}

// DashboardPage is the signed-in user's profile: identity header, a GitHub-style
// contribution graph, their repositories, and recent pull-request activity.
export default async function DashboardPage() {
  const [user, avatarFromIdp, token, meta] = await Promise.all([
    requireSession(),
    getProfileAvatar(),
    getToken(),
    getMeta(),
  ]);

  const projects = token ? await getMyProjects(token) : [];
  const hasOrgProjects = projects.some((p) => !p.isPersonal);

  type RepoWithProject = { repo: Repo; project: MyProject };
  const perProject = token
    ? await Promise.all(
        projects.map(async (p) => {
          const repos = await listReposByProject(token, p.slug);
          return repos.map<RepoWithProject>((r) => ({ repo: r, project: p }));
        }),
      )
    : [];
  const repos = perProject.flat();

  const [contributions, myPulls] = token
    ? await Promise.all([
        getMyContributions(token),
        getMyPulls(token, { state: "all" }),
      ])
    : [[], { ok: false as const, status: 0, message: "" }];

  const recentPulls = myPulls.ok ? myPulls.data.pulls.slice(0, 6) : [];
  const openPRs = myPulls.ok ? myPulls.data.pulls.filter((p) => p.pull.state === "open").length : 0;
  const avatarUrl = avatarFromIdp;

  return (
    <>
      {meta === null && (
        <div className="banner">
          Can&apos;t reach the Quill backend. Start it with{" "}
          <span className="mono">make be-run</span> or{" "}
          <span className="mono">make up</span>.
        </div>
      )}

      {/* Profile header */}
      <div className="profile-head">
        {avatarUrl ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img className="profile-avatar" src={avatarUrl} alt={user.displayName} />
        ) : (
          <div className="profile-avatar profile-avatar--fallback">{initials(user.displayName)}</div>
        )}
        <div className="profile-id">
          <h1 className="profile-name">
            {user.displayName}
            {user.isAdmin && <span className="tag">admin</span>}
          </h1>
          <div className="profile-meta">
            <span className="profile-handle">@{user.username}</span>
            {user.createdAt && <span className="profile-joined">· joined {joinedLabel(user.createdAt)}</span>}
          </div>
        </div>
        <span className="spacer" />
        <div className="profile-stats">
          <div className="profile-stat">
            <b>{repos.length}</b>
            <span>repos</span>
          </div>
          <div className="profile-stat">
            <b>{projects.length}</b>
            <span>projects</span>
          </div>
          <div className="profile-stat">
            <b>{openPRs}</b>
            <span>open PRs</span>
          </div>
        </div>
      </div>

      {/* Contribution graph */}
      <div className="panel">
        <ContributionGraph data={contributions} />
      </div>

      <div className="profile-cols">
        {/* Repositories */}
        <div className="panel">
          <h2>
            Repositories
            <span className="tag">{repos.length}</span>
            <span className="spacer" />
            <Link className="btn ghost small" href="/repositories">
              View all
            </Link>
          </h2>
          {repos.length === 0 ? (
            <div className="empty">
              No repositories yet.{" "}
              <Link href="/repositories">Create or import one</Link> to get started.
            </div>
          ) : (
            repos.slice(0, 8).map(({ repo, project }) => (
              <Link
                className="row-item"
                key={repo.id}
                href={`/${encodeURIComponent(project.slug)}/${encodeURIComponent(repo.slug)}`}
              >
                <span className="tree-icon dir">◆</span>
                <div className="pr-main">
                  <span className="nm">{repo.name}</span>
                  {hasOrgProjects && <span className="sub">{project.name}</span>}
                </div>
                <span className="spacer" />
                {repo.starCount > 0 && <span className="sub">★ {repo.starCount}</span>}
                <VisibilityBadge visibility={repo.visibility} />
              </Link>
            ))
          )}
        </div>

        {/* Recent activity */}
        <div className="panel">
          <h2>Recent activity</h2>
          {recentPulls.length === 0 ? (
            <div className="empty">No pull request activity yet.</div>
          ) : (
            recentPulls.map(({ projectSlug, repoSlug, repoName, pull }) => (
              <Link
                className="row-item"
                key={`${projectSlug}/${repoSlug}#${pull.number}`}
                href={`/${encodeURIComponent(projectSlug)}/${encodeURIComponent(repoSlug)}/pulls/${pull.number}`}
              >
                <span className="tree-icon">
                  {pull.merged ? "⬡" : pull.state === "open" ? "↳" : "✕"}
                </span>
                <div className="pr-main">
                  <span className="nm">{pull.title}</span>
                  <span className="sub">
                    {repoName} · #{pull.number} · {pull.merged ? "merged" : pull.state}
                  </span>
                </div>
              </Link>
            ))
          )}
        </div>
      </div>
    </>
  );
}
