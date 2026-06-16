"use client";

import { useRouter } from "next/navigation";

function treeHref(project: string, repo: string, ref: string, path = "") {
  const slugParts = [...ref.split("/"), ...(path ? path.split("/") : [])];

  const slug = slugParts
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join("/");

  return `/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(
    repo,
  )}/tree/${slug}`;
}

export function BranchSelector({
  project,
  repo,
  selectedBranch,
  branches,
  path,
}: {
  project: string;
  repo: string;
  selectedBranch: string;
  branches: string[];
  path: string;
}) {
  const router = useRouter();

  return (
    <div className="branch-selector">
      <span className="ic" aria-hidden="true">
        ⎇
      </span>
      <select
        id="branch-select"
        aria-label="Switch branch"
        value={selectedBranch}
        onChange={(e) => {
          router.push(treeHref(project, repo, e.target.value, path));
        }}
      >
        {branches.map((branch) => (
          <option key={branch} value={branch}>
            {branch}
          </option>
        ))}
      </select>
    </div>
  );
}
