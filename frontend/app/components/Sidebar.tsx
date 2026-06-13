"use client";

// Sidebar is the primary navigation chrome. It highlights the active route and,
// for now, renders a placeholder identity + sign-out link. Real auth state and
// org switching arrive in PR 3 (auth) and PR 4 (Forgejo orgs).

import Link from "next/link";
import { usePathname } from "next/navigation";

export type NavItem = {
  href: string;
  label: string;
  icon: string;
  soon?: boolean;
};

const NAV: NavItem[] = [
  { href: "/", label: "Dashboard", icon: "◧" },
  { href: "/repos", label: "Repositories", icon: "▤", soon: true },
  { href: "/pulls", label: "Pull requests", icon: "⤭", soon: true },
  { href: "/pipelines", label: "Pipelines", icon: "▷", soon: true },
  { href: "/teams", label: "Teams", icon: "◎", soon: true },
  { href: "/settings", label: "Settings", icon: "⚙", soon: true },
];

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  return pathname === href || pathname.startsWith(href + "/");
}

export function Sidebar() {
  const pathname = usePathname() || "/";

  return (
    <aside className="side">
      <div className="brand">
        <span className="dot" /> Quill
      </div>

      <div className="org">
        <span className="who">Signed in as</span>
        <b>dev@quill.local</b>
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

      <div className="foot">
        <Link className="logout-btn" href="/login">
          Sign out
        </Link>
        <div className="copy">© 2026 Quill</div>
      </div>
    </aside>
  );
}
