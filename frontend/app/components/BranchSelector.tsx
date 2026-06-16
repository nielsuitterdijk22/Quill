"use client";

import { useRouter } from "next/navigation";

function treeHref(org: string, repo: string, ref: string, path = "") {
  const slugParts = [...ref.split("/"), ...(path ? path.split("/") : [])];

  const slug = slugParts
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join("/");

  return `/orgs/${encodeURIComponent(org)}/repos/${encodeURIComponent(
    repo,
  )}/tree/${slug}`;
}

export function BranchSelector({
  org,
  repo,
  selectedBranch,
  branches,
  path,
}: {
  org: string;
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
          router.push(treeHref(org, repo, e.target.value, path));
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
