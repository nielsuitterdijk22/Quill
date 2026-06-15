"use client";

import { useRouter } from "next/navigation";

import type { Org } from "../../lib/api";

// persistDefaultOrg mirrors lib/orgs#setDefaultOrg but is inlined here so this
// client component doesn't pull in lib/orgs (which transitively imports the
// server-only session helpers).
function persistDefaultOrg(org: string) {
  if (globalThis.window !== undefined) {
    localStorage.setItem("defaultOrg", org);
  }
}

// OrgFilter scopes the Repositories list to one of the organizations the user
// is authorized for. Changing it navigates to ?org={slug}, which the server
// page validates against the authorized org set before listing repos, so the
// select both filters the view and carries the authorization boundary.
export function OrgFilter({
  orgs,
  selected,
}: {
  orgs: Org[];
  selected: string;
}) {
  const router = useRouter();

  function onChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const slug = e.target.value;
    persistDefaultOrg(slug);
    router.push(`/repositories?org=${encodeURIComponent(slug)}`);
  }

  return (
    <label className="org-filter">
      <span className="org-filter-label">Organization</span>
      <select value={selected} onChange={onChange} aria-label="Organization">
        {orgs.map((o) => (
          <option key={o.id} value={o.slug}>
            {o.name}
          </option>
        ))}
      </select>
    </label>
  );
}
