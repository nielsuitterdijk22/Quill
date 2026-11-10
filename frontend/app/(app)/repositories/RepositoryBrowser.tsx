"use client";

import { useMemo, useState } from "react";
import Link from "next/link";

import type { Repo } from "@/lib/api";
import { VisibilityBadge } from "@/components/repo";

type RepoRow = { repo: Repo; projectSlug: string; projectName: string; showProject: boolean };

// RepositoryBrowser renders the repositories list with a client-side search
// field (name + project). Server-rendered rows serialised in; filtering is
// purely local so it feels instant, with a `t` shortcut to focus (matches the
// keyboard-shortcuts overlay: press t anywhere to jump to the filter).
export function RepositoryBrowser({
  rows,
  newRepoHref,
}: {
  rows: RepoRow[];
  newRepoHref: string;
}) {
  const [q, setQ] = useState("");

  const filtered = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return rows;
    return rows.filter(
      (r) =>
        r.repo.name.toLowerCase().includes(t) ||
        r.repo.slug.toLowerCase().includes(t) ||
        r.projectName.toLowerCase().includes(t),
    );
  }, [rows, q]);

  return (
    <div className="panel">
      <h2>
        All Repositories
        <span className="tag">{rows.length}</span>
        <span className="spacer" />
        <input
          className="repo-filter"
          data-repo-filter
          placeholder="Search repositories…  (t)"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </h2>
      {rows.length === 0 ? null : filtered.length === 0 ? (
        <div className="empty">No repositories match &ldquo;{q}&rdquo;.</div>
      ) : (
        filtered.map(({ repo, projectSlug, projectName, showProject }) => (
          <Link
            className="row-item"
            key={repo.id}
            href={`/${encodeURIComponent(projectSlug)}/${encodeURIComponent(repo.slug)}`}
          >
            <span className="tree-icon dir">◆</span>
            <div className="pr-main">
              <span className="nm">{repo.name}</span>
              <span className="sub">
                {repo.description || (showProject ? projectName : "")}
              </span>
            </div>
            <span className="spacer" />
            {repo.updatedAt && (
              <span className="when sub">Updated {relTime(repo.updatedAt)}</span>
            )}
            <VisibilityBadge visibility={repo.visibility} />
          </Link>
        ))
      )}
    </div>
  );
}

function relTime(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (Number.isNaN(s)) return "";
  if (s < 45) return "just now";
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}
