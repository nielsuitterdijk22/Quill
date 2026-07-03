// Marks for the tool suite (Atlas, Quill, Tempo, Forge). Each keeps the same
// rounded-tile + 135deg gradient treatment as the old brand dot, but with its
// own hue drawn from a color already in the shared design tokens (globals.css)
// so the switcher reads at a glance without inventing new brand colors.
export type AppId = "atlas" | "quill" | "tempo" | "forge";

export const APP_META: Record<AppId, { label: string; tag: string; gradient: [string, string] }> = {
  quill: { label: "Quill", tag: "Version control", gradient: ["#7c5cff", "#44aadd"] },
  atlas: { label: "Atlas", tag: "Developer portal", gradient: ["#44aadd", "#2dd4bf"] },
  tempo: { label: "Tempo", tag: "Work tracking", gradient: ["#f2b544", "#d29922"] },
  forge: { label: "Forge", tag: "CI runners", gradient: ["#ff8a4c", "#f0506e"] },
};

function QuillGlyph() {
  return (
    <svg width="60%" height="60%" viewBox="0 0 24 24" fill="none">
      <path d="M5 16.5C9 9.5 13.5 6 19 4" stroke="#fff" strokeWidth={1.8} strokeLinecap="round" />
      <circle cx="5" cy="16.5" r="1.4" fill="#fff" />
      <path d="M18.5 5.3L15.7 7.6" stroke="#fff" strokeWidth={1.4} strokeLinecap="round" />
      <path d="M16 7.6L13.4 10" stroke="#fff" strokeWidth={1.4} strokeLinecap="round" />
    </svg>
  );
}

function AtlasGlyph() {
  return (
    <svg width="60%" height="60%" viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="12" r="8" stroke="#fff" strokeWidth={1.6} />
      <ellipse cx="12" cy="12" rx="8" ry="3.1" stroke="#fff" strokeWidth={1.6} />
      <ellipse cx="12" cy="12" rx="3.1" ry="8" stroke="#fff" strokeWidth={1.6} />
    </svg>
  );
}

function TempoGlyph() {
  return (
    <svg width="60%" height="60%" viewBox="0 0 24 24" fill="none">
      <path d="M5 18V13" stroke="#fff" strokeWidth={2.4} strokeLinecap="round" />
      <path d="M10 18V6" stroke="#fff" strokeWidth={2.4} strokeLinecap="round" />
      <path d="M15 18V10" stroke="#fff" strokeWidth={2.4} strokeLinecap="round" />
      <path d="M19.5 18V8" stroke="#fff" strokeWidth={2.4} strokeLinecap="round" />
    </svg>
  );
}

function ForgeGlyph() {
  return (
    <svg width="60%" height="60%" viewBox="0 0 24 24" fill="none">
      <path
        d="M12 2.5C8.5 7 6.5 10 6.5 13.8C6.5 18 8.9 21 12 21C15.1 21 17.5 18 17.5 13.8C17.5 10 15.5 7 12 2.5Z"
        fill="#fff"
        fillOpacity={0.35}
      />
      <path
        d="M12 9C10.3 11.6 9.6 13.2 9.6 15.1C9.6 17.3 10.6 19 12 19C13.4 19 14.4 17.3 14.4 15.1C14.4 13.2 13.7 11.6 12 9Z"
        fill="#fff"
      />
    </svg>
  );
}

const GLYPHS: Record<AppId, () => JSX.Element> = {
  quill: QuillGlyph,
  atlas: AtlasGlyph,
  tempo: TempoGlyph,
  forge: ForgeGlyph,
};

export function AppTile({ app, size = 32 }: { app: AppId; size?: number }) {
  const [a, b] = APP_META[app].gradient;
  const Glyph = GLYPHS[app];
  return (
    <div
      style={{
        width: size,
        height: size,
        borderRadius: size * 0.28,
        background: `linear-gradient(135deg, ${a}, ${b})`,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        flex: "none",
      }}
    >
      <Glyph />
    </div>
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
