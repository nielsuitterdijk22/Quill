// RepoChip names a repository with a stable colour, so lists that mix repos
// (or two repos with the same workflow names, like quill and tempo) read
// apart at a glance. The hue derives from the full name; text stays in the
// text tokens, the colour lives in the initial's tile only.
function hueOf(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return h % 360;
}

export function RepoChip({
  project,
  repo,
  showProject = true,
  size = "md",
}: {
  project: string;
  repo: string;
  showProject?: boolean;
  size?: "md" | "lg";
}) {
  const name = `${project}/${repo}`;
  return (
    <span className={`repo-chip ${size}`} title={name}>
      <span
        className="repo-chip-tile"
        aria-hidden
        style={{ "--chip-h": hueOf(name) } as React.CSSProperties}
      >
        {repo.charAt(0).toUpperCase()}
      </span>
      <span className="repo-chip-name">
        {showProject && <span className="repo-chip-project">{project}/</span>}
        <b>{repo}</b>
      </span>
    </span>
  );
}
