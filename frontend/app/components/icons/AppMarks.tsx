// Marks for the tool suite (Atlas, Quill, Tempo, Forge), in the suite's
// "ink & gold" identity (2026-11-14): every app is the same dark ink tile as
// Quill's favicon, carrying its own glyph in its own hue. Quill's glyph is the
// favicon's gold feather (public/favicon.svg), so the switcher, the sidebar
// brand and the browser tab all show one mark. The tile stays ink in both
// themes (it is a logo, not a surface); a hairline ring keeps it visible on
// the dark sidebar.
export type AppId = "atlas" | "quill" | "tempo" | "forge";

export const APP_META: Record<AppId, { label: string; tag: string; gradient: [string, string] }> = {
  quill: { label: "Quill", tag: "Version control", gradient: ["#e8b95a", "#b9822e"] },
  atlas: { label: "Atlas", tag: "Developer portal", gradient: ["#5cc4f0", "#2dd4bf"] },
  tempo: { label: "Tempo", tag: "Work tracking", gradient: ["#f2b544", "#d29922"] },
  forge: { label: "Forge", tag: "CI runners", gradient: ["#ff8a4c", "#f0506e"] },
};

const INK = "#12141c";

// Glyphs are drawn on the tile's 24x24 grid; `paint` is the app's gradient.
function QuillGlyph({ paint }: { paint: string }) {
  return (
    <path
      d="M17.5 4.5c-4.2.6-7.6 3-9.2 6.8-.5 1.2-.8 2.6-.8 4.1l-.7 2.4c-.1.4.3.8.7.7l2.4-.7c1.5 0 2.9-.3 4.1-.8 3.8-1.6 6.2-5 6.8-9.2l.2-1.6-1.5-.2-1.4-.2-.2 1.5c-.5 3.4-2.3 6-5.2 7.3-.9.4-1.9.6-3 .6l1.6-1.6c1-1 1.9-2.4 2.4-3.9.2-.5-.4-1-.9-.7-1.5.6-2.8 1.5-3.8 2.5l-1.5 1.5c0-1 .2-2 .6-2.9C10.6 8.4 13.2 6.6 16.6 6l1.5-.2-.6-1.3z"
      fill={paint}
      fillRule="evenodd"
    />
  );
}

function AtlasGlyph({ paint }: { paint: string }) {
  return (
    <g fill="none" stroke={paint} strokeWidth={1.5}>
      <circle cx="12" cy="12" r="6.6" />
      <ellipse cx="12" cy="12" rx="6.6" ry="2.6" />
      <ellipse cx="12" cy="12" rx="2.6" ry="6.6" />
    </g>
  );
}

function TempoGlyph({ paint }: { paint: string }) {
  return (
    <g stroke={paint} strokeWidth={2.2} strokeLinecap="round">
      <path d="M7 17.5V13.5" />
      <path d="M10.7 17.5V7" />
      <path d="M14.4 17.5V10.5" />
      <path d="M18 17.5V8.5" />
    </g>
  );
}

function ForgeGlyph({ paint }: { paint: string }) {
  return (
    <g fill={paint}>
      <path
        d="M12 4.2C9.3 7.7 7.7 10 7.7 13c0 3.2 1.9 5.6 4.3 5.6s4.3-2.4 4.3-5.6c0-3-1.6-5.3-4.3-8.8z"
        fillOpacity={0.4}
      />
      <path d="M12 9.3c-1.3 2-1.9 3.3-1.9 4.8 0 1.7.8 3 1.9 3s1.9-1.3 1.9-3c0-1.5-.6-2.8-1.9-4.8z" />
    </g>
  );
}

const GLYPHS: Record<AppId, (p: { paint: string }) => JSX.Element> = {
  quill: QuillGlyph,
  atlas: AtlasGlyph,
  tempo: TempoGlyph,
  forge: ForgeGlyph,
};

export function AppTile({ app, size = 32 }: { app: AppId; size?: number }) {
  const [a, b] = APP_META[app].gradient;
  const Glyph = GLYPHS[app];
  // Per-app id: identical definitions when a mark repeats on a page, so a
  // duplicate id resolves to the same gradient.
  const gid = `appmark-${app}`;
  return (
    <svg
      className="app-tile"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <defs>
        {/* userSpaceOnUse: objectBoundingBox paints nothing on zero-width
            shapes (Tempo's vertical bars). */}
        <linearGradient id={gid} gradientUnits="userSpaceOnUse" x1="4" y1="4" x2="20" y2="20">
          <stop offset="0" stopColor={a} />
          <stop offset="1" stopColor={b} />
        </linearGradient>
      </defs>
      <rect width="24" height="24" rx="6.7" fill={INK} />
      <rect x="0.25" y="0.25" width="23.5" height="23.5" rx="6.45" fill="none" stroke="#fff" strokeOpacity={0.08} strokeWidth={0.5} />
      <Glyph paint={`url(#${gid})`} />
    </svg>
  );
}

// The switcher trigger: a literal 2x2 grid, echoing the four apps it opens.
export function WaffleIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
      <rect x="3" y="3" width="8" height="8" rx="2" />
      <rect x="13" y="3" width="8" height="8" rx="2" />
      <rect x="3" y="13" width="8" height="8" rx="2" />
      <rect x="13" y="13" width="8" height="8" rx="2" />
    </svg>
  );
}
