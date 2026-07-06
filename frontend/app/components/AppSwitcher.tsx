"use client";

// Top-left app switcher — jumps between this suite's sibling tools. Ported
// pattern from Quill's own ProjectSwitcher (button + absolutely-positioned
// menu, closed on outside click or Escape), swapped to a 2x2 grid of app
// tiles.
import { useEffect, useRef, useState } from "react";

import { APP_META, AppTile, WaffleIcon, type AppId } from "./icons/AppMarks";

// Sibling URLs are build-time env vars (see frontend/.env.example) so each
// deployment points at its own domains. A tile with no URL configured still
// shows, disabled, rather than silently disappearing from the grid.
const URLS: Record<AppId, string | undefined> = {
  atlas: process.env.NEXT_PUBLIC_ATLAS_URL,
  quill: process.env.NEXT_PUBLIC_QUILL_URL,
  tempo: process.env.NEXT_PUBLIC_TEMPO_URL,
  forge: process.env.NEXT_PUBLIC_FORGE_URL,
};

const ORDER: AppId[] = ["atlas", "quill", "tempo", "forge"];

export function AppSwitcher({ current }: { current: AppId }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onOutside);
    document.addEventListener("keydown", onEscape);
    return () => {
      document.removeEventListener("mousedown", onOutside);
      document.removeEventListener("keydown", onEscape);
    };
  }, [open]);

  return (
    <div className="app-switcher" ref={ref}>
      <button
        type="button"
        className={open ? "app-switcher-btn open" : "app-switcher-btn"}
        aria-label="Switch app"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <WaffleIcon />
      </button>
      {open && (
        <div className="app-switcher-menu" role="menu">
          <div className="app-switcher-title">Switch to</div>
          <div className="app-switcher-grid">
            {ORDER.map((id) => {
              const meta = APP_META[id];
              if (id === current) {
                return (
                  <div key={id} className="app-switcher-item current">
                    <AppTile app={id} size={40} />
                    <div className="label">{meta.label}</div>
                    <div className="tag">current</div>
                  </div>
                );
              }
              const url = URLS[id];
              if (!url) {
                return (
                  <div
                    key={id}
                    className="app-switcher-item disabled"
                    title={`Set NEXT_PUBLIC_${id.toUpperCase()}_URL to enable`}
                  >
                    <AppTile app={id} size={40} />
                    <div className="label">{meta.label}</div>
                    <div className="tag">{meta.tag}</div>
                  </div>
                );
              }
              return (
                <a key={id} className="app-switcher-item" href={url}>
                  <AppTile app={id} size={40} />
                  <div className="label">{meta.label}</div>
                  <div className="tag">{meta.tag}</div>
                </a>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
