import Link from "next/link";

import type { Branch } from "../lib/api";
import { timeAgo } from "../lib/timeago";
import { repoBase } from "./repo";

// A branch counts as "recently pushed" for a day, like the GitHub/Azure DevOps
// "Compare & pull request" prompt, and only while it has commits the default
// branch lacks and no open pull request already carries it.
const RECENT_MS = 24 * 60 * 60 * 1000;
const MAX_PROMPTS = 3;

// recentBranches picks the branches worth a "create pull request" prompt,
// newest first. Exported for the pages that already hold the data.
export function recentBranches(
  branches: Branch[],
  defaultBranch: string,
  openHeads: string[],
  now: number = Date.now(),
): Branch[] {
  const open = new Set(openHeads);
  return branches
    .filter((b) => {
      if (b.name === defaultBranch || open.has(b.name)) return false;
      if (!b.ahead || b.ahead < 1 || !b.updatedAt) return false;
      const t = new Date(b.updatedAt).getTime();
      return !Number.isNaN(t) && now - t <= RECENT_MS;
    })
    .sort(
      (a, b) =>
        new Date(b.updatedAt ?? 0).getTime() -
        new Date(a.updatedAt ?? 0).getTime(),
    )
    .slice(0, MAX_PROMPTS);
}

// RecentBranchPrompt offers a one-click "Compare & pull request" for branches
// that just received commits and have no open pull request yet.
export function RecentBranchPrompt({
  project,
  repo,
  defaultBranch,
  branches,
  openHeads,
}: {
  project: string;
  repo: string;
  defaultBranch: string;
  branches: Branch[];
  openHeads: string[];
}) {
  const recent = recentBranches(branches, defaultBranch, openHeads);
  if (recent.length === 0) return null;
  const base = repoBase(project, repo);
  return (
    <div className="pr-prompts">
      {recent.map((b) => (
        <div className="pr-prompt" key={b.name}>
          <span className="tree-icon">⎇</span>
          <span className="pr-prompt-text">
            <span className="nm">{b.name}</span> had recent pushes{" "}
            {b.updatedAt ? timeAgo(b.updatedAt) : ""}
          </span>
          <Link
            className="btn primary"
            href={`${base}/pulls/new?head=${encodeURIComponent(b.name)}`}
          >
            Compare &amp; pull request
          </Link>
        </div>
      ))}
    </div>
  );
}
