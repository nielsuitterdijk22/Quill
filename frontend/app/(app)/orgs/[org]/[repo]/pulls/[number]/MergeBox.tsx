"use client";

import { useFormState, useFormStatus } from "react-dom";

import { mergeAction, type MergeState } from "./actions";

const initialState: MergeState = {};

function MergeButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Merging…" : "Merge pull request"}
    </button>
  );
}

// MergeBox lets a member merge an open PR with a chosen strategy.
export function MergeBox({
  org,
  repo,
  number,
  mergeable,
}: {
  org: string;
  repo: string;
  number: number;
  mergeable: boolean;
}) {
  const action = mergeAction.bind(null, org, repo, number);
  const [state, formAction] = useFormState(action, initialState);

  return (
    <div className="panel merge-box">
      <div className="merge-head">
        <span className={`merge-dot ${mergeable ? "ok" : "warn"}`} />
        <strong>
          {mergeable
            ? "This branch has no conflicts with the base branch."
            : "This pull request may have conflicts."}
        </strong>
      </div>
      {state.error && <div className="form-error">{state.error}</div>}
      <form className="merge-form" action={formAction}>
        <select name="method" defaultValue="merge">
          <option value="merge">Create a merge commit</option>
          <option value="squash">Squash and merge</option>
          <option value="rebase">Rebase and merge</option>
        </select>
        <MergeButton />
      </form>
    </div>
  );
}
