"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const SECTIONS = [
  { slug: "members", label: "Members", hint: "People and invitations" },
  { slug: "groups", label: "Groups", hint: "Teams and project access" },
  { slug: "sign-in", label: "Sign-in & provisioning", hint: "SSO and SCIM", admin: true },
  { slug: "billing", label: "Billing", hint: "Plan and usage", admin: true },
  { slug: "policies", label: "Policies", hint: "Branches, deploys, egress" },
];

// SettingsNav is the organization settings sub-navigation.
export function SettingsNav({ org, canEdit }: { org: string; canEdit: boolean }) {
  const path = usePathname();
  return (
    <nav className="org-settings-nav" aria-label="Organization settings">
      {SECTIONS.filter((s) => canEdit || !s.admin).map((s) => {
        const href = `/orgs/${encodeURIComponent(org)}/settings/${s.slug}`;
        const active = path === href || path.startsWith(href + "/");
        return (
          <Link key={s.slug} href={href} className={`org-settings-nav-item${active ? " active" : ""}`}>
            <span className="org-settings-nav-label">{s.label}</span>
            <span className="org-settings-nav-hint">{s.hint}</span>
          </Link>
        );
      })}
    </nav>
  );
}
