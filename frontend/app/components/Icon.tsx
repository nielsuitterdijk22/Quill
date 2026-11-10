// Quill's icon set: geometric SVG glyphs in the H5 language — mostly linear
// ink (currentColor inherits from the parent nav/lint state), with select
// accent highlights via CSS classes (see .icon-acc / .icon-gold in globals).
// Two sizes: 16 (nav, tabs, toolbar) and 14 (inline, badges). Icons are
// inline SVGs to avoid external icon-font requests.
const S: React.SVGProps<SVGPathElement> = { fill: "none", stroke: "currentColor", strokeWidth: 1.5, strokeLinecap: "round", strokeLinejoin: "round" };

const ICONS: Record<string, string> = {
  dashboard: "M3 10h8l1 5M3 10V7a1 1 0 0 1 1-1h3l2 2h5v2h2v4H3zM8 6V3l2 2h3a1 1 0 0 1 1 1",
  repos: "M12 3a3 3 0 0 0-3 3c0 1.5 1.5 3 3 3s3-1.5 3-3a3 3 0 0 0-3-3zM5 13c0-2 2-4 7-4s7 2 7 4v1H5z",
  pulls: "M4 12h6M4 10h4a1 1 0 0 1 1 1v1M10 5a3 3 0 1 0 6 0 3 3 0 0 0-6 0M13 2v6M4 5h4v7h6",
  pipelines: "M12 5l2 2-2 2M4 7h9a1 1 0 0 1 1 1M7 11a3 3 0 1 0 6 0",
  projects: "M4 3a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1V3zM9 3a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1V3zM14 5v7a1 1 0 0 1-1 1V6M15 5h-4v2h4",
  issues: "M8 1a7 7 0 1 0 0 14A7 7 0 0 0 8 1zM8 5v3M8 10v1",
  settings: "M8 4a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM8 1v2M8 12v2M1 8h2M12 8h2M3 3l1 1M12 12l1 1M3 12l1-1M12 3l1 1",
  commits: "M15 4H5.5A2.5 2.5 0 0 0 3 6.5v3A2.5 2.5 0 0 0 5.5 12H15M15 4v1M3 8a2 2 0 1 0 4 0 2 2 0 0 0-4 0",
  star: "M8 2l1.4 3.2 3.4.4-2.5 2.3.7 3.4L8 9.6l-3 1.7.7-3.4L3.2 5.6l3.4-.4z",
  fork: "M8 3a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3zM3 12a1.5 1.5 0 1 0 3 0 1.5 1.5 0 0 0-3 0zM10 12a1.5 1.5 0 1 0 3 0 1.5 1.5 0 0 0-3 0M8 4.5V8m0 0c-2 0-3 1-3 2.5M8 8c2 0 3 1 3 2.5",
  code: "M7 3L3 8l4 5M9 3l4 5-4 5",
  branches: "M5 3a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3zM11 3a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3zM5 14a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3zM5 6v5M5 8h6a1 1 0 0 1 1 1v2",
  farm: "M10 5a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM10 5V2M10 7a3 3 0 0 0 3 3M4 3a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3zM4 6v2c0 1 .5 2 2 2h4a2 2 0 0 0 2 2",
  security: "M8 2a3 3 0 0 0-3 3v2a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3zM8 2v12M5 8H3m10 0h-2M8 10v4",
  billing: "M2 4a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1zM2 6h12M4 9h4l-1 2h5L11 9h1",
  data: "M13 4a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0zM13 12a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0zM3 4a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3zM3 10.5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3zM4.5 5.5l5.5 0M4.5 11.5l5.5-5M9.5 12.5a1.5 1.5 0 0 0 3 1v-7",
  clone_url: "M9 5H5a1 1 0 0 0-1 1v7a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V9M9 5V3h4v4h-2M3 13V5a1 1 0 0 1 1-1h4",
  checkmark: "M3 8l3 3 7-7",
  close: "M4 4l8 8M12 4l-8 8",
  search: "M11 9a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0zM10.5 10.5L14 14",
  arrow_right: "M2 8h10M9 4l4 4-4 4",
  arrow_left: "M14 8H4M7 4L2 8l4 4",
  arrow_down: "M8 2v10M4 8l4 4 4-4",
  external: "M10 4h4M14 4v4M14 4l-7 7M12 8v5a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5",
  user: "M8 2a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM2 14c0-3 2-5 6-5s6 2 6 5",
  lock: "M5 8V5a3 3 0 0 1 6 0v3M5 8h6a1 1 0 0 1 1 1v4a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1zM8 10v2",
  eye: "M8 3C3 3 1 8 1 8s2 5 7 5 7-5 7-5-2-5-7-5zM8 6a2 2 0 1 1 0 4 2 2 0 0 1 0-4z",
  edit: "M8 6L3 14h4l3-6M12 3l2 2-6 8H4l2-2z",
  briefcase: "M2 8c2-2 3-3 6-3s4 1 6 3v4a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1zM6 5V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1",
  git: "M14 10a2 2 0 1 1-4 0 2 2 0 0 1 4 0zM6 10a2 2 0 1 1-4 0 2 2 0 0 1 4 0zM6 5a2 2 0 1 1-4 0 2 2 0 0 1 4 0zM4 7v1c0 1 .5 2 4 2M12 8V7c0-2-2-5-6-5",
};

export function Icon({ name, size = 16, className = "" }: { name: string; size?: number; className?: string }) {
  const path = ICONS[name];
  if (!path) return null;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      className={`icon ${className}`}
      aria-hidden="true"
    >
      <path d={path} {...S} />
    </svg>
  );
}
