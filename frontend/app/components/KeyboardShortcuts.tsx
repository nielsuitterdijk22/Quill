"use client";

import { useEffect, useState } from "react";

const SHORTCUTS = [
  { key: "j", desc: "Move selection down in lists" },
  { key: "k", desc: "Move selection up in lists" },
  { key: "Enter", desc: "Open selected item" },
  { key: "?", desc: "Show this help" },
  { key: "Esc", desc: "Close this dialog" },
];

// Selects all navigatable row links in the current page.
function getNavItems(): HTMLAnchorElement[] {
  return Array.from(
    document.querySelectorAll<HTMLAnchorElement>(
      ".row-item a:first-child, .pr-row a.nm, .row-item:first-child > a",
    ),
  );
}

// getNavRows returns the closest .row-item parent for each focusable anchor,
// falling back to the anchors themselves when there's no row wrapper.
function getNavRows(): Element[] {
  const rows = Array.from(
    document.querySelectorAll<Element>(".row-item"),
  );
  return rows.length > 0 ? rows : getNavItems();
}

export function KeyboardShortcuts() {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState(-1);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const tag = (e.target as HTMLElement)?.tagName ?? "";
      const inInput = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";

      if (e.key === "?" && !inInput) {
        setOpen((v) => !v);
        return;
      }
      if (e.key === "Escape") {
        setOpen(false);
        setSelected(-1);
        return;
      }
      if (inInput) return;

      if (e.key === "j" || e.key === "k") {
        e.preventDefault();
        const rows = getNavRows();
        if (rows.length === 0) return;
        setSelected((prev) => {
          const next =
            e.key === "j"
              ? Math.min(prev + 1, rows.length - 1)
              : Math.max(prev - 1, 0);
          rows[next]?.scrollIntoView({ block: "nearest" });
          (rows[next] as HTMLElement)?.focus?.();
          const link = rows[next]?.querySelector("a");
          link?.focus();
          return next;
        });
      }
      if (e.key === "Enter" && selected >= 0) {
        const rows = getNavRows();
        const link = rows[selected]?.querySelector<HTMLAnchorElement>("a");
        link?.click();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selected]);

  if (!open) return null;

  return (
    <div className="kb-overlay" onClick={() => setOpen(false)}>
      <div className="kb-panel" onClick={(e) => e.stopPropagation()}>
        <h2>Keyboard shortcuts</h2>
        <div className="kb-list">
          {SHORTCUTS.map((s) => (
            <div key={s.key} className="kb-row">
              <kbd>{s.key}</kbd>
              <span className="desc">{s.desc}</span>
            </div>
          ))}
        </div>
        <div className="kb-close">
          <button type="button" className="btn ghost" onClick={() => setOpen(false)}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
