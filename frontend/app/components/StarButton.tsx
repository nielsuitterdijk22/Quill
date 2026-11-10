import { toggleStarAction } from "../lib/actions";

// StarButton is a plain progressive-enhancement form: the star/unstar runs
// as a server action with the session's token, so it works regardless of
// the browser-side proxy. Renders GitHub's two-state chip with the count.
export function StarButton({
  project,
  repo,
  starCount,
  starred,
}: {
  project: string;
  repo: string;
  starCount: number;
  starred: boolean;
}) {
  return (
    <form action={toggleStarAction}>
      <input type="hidden" name="project" value={project} />
      <input type="hidden" name="repo" value={repo} />
      <input type="hidden" name="starred" value={starred ? "1" : "0"} />
      <button
        className={`btn${starred ? "" : " primary"}`}
        type="submit"
        title={starred ? "Unstar this repository" : "Star this repository"}
      >
        {starred ? "★" : "☆"} {starCount}
      </button>
    </form>
  );
}
