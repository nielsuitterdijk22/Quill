"use client";

// Sidebar is the primary navigation chrome. It highlights the active route and
// renders the signed-in user plus a sign-out control wired to a server action.

import Link from "next/link";
import { usePathname } from "next/navigation";

import { logoutAction } from "../lib/actions";
import type { User } from "../lib/api";

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

export function Sidebar({ user }: { user: User }) {
  const pathname = usePathname() || "/";

  return (
    <aside className="side">
      <div className="brand">
        <span className="dot" /> Quill
      </div>

      <div className="org">
        <span className="who">Signed in as</span>
        <b>{user.displayName || user.username}</b>
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
        <form action={logoutAction}>
          <button className="logout-btn" type="submit">
            Sign out
          </button>
        </form>
        <div className="copy">© 2026 Quill</div>
      </div>
    </aside>
  );
}
