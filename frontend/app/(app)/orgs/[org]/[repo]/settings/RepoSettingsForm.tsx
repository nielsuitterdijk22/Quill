"use client";

import { useFormState, useFormStatus } from "react-dom";

import type { Repo } from "../../../../../lib/api";
import { updateRepoSettingsAction, type RepoSettingsFormState } from "./actions";

const initial: RepoSettingsFormState = {};

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : "Save changes"}
    </button>
  );
}

// RepoSettingsForm edits a repository's general metadata: display name,
// description, visibility, and default branch. The default-branch picker is
// only shown when the repository already has branches.
export function RepoSettingsForm({
  org,
  repo,
  repository,
  branchNames,
}: {
  org: string;
  repo: string;
  repository: Repo;
  branchNames: string[];
}) {
  const action = updateRepoSettingsAction.bind(null, org, repo);
  const [state, formAction] = useFormState(action, initial);

  return (
    <div className="panel form-narrow">
      {state.error && <div className="form-error">{state.error}</div>}
      {state.ok && <div className="form-success">Settings saved.</div>}
      <form action={formAction}>
        <label className="field">
          <span>Display name</span>
          <input name="name" defaultValue={repository.name} required />
        </label>
        <label className="field">
          <span>Description</span>
          <textarea
            name="description"
            rows={3}
            defaultValue={repository.description}
            placeholder="Optional"
          />
        </label>
        <label className="field">
          <span>Visibility</span>
          <select name="visibility" defaultValue={repository.visibility}>
            <option value="private">Private</option>
            <option value="internal">Internal</option>
            <option value="public">Public</option>
          </select>
        </label>
        {branchNames.length > 0 ? (
          <label className="field">
            <span>Default branch</span>
            <select name="defaultBranch" defaultValue={repository.defaultBranch}>
              {branchNames.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
        ) : (
          <p className="hint">
            Push a branch to this repository to choose a default branch.
          </p>
        )}
        <div className="form-actions">
          <SaveButton />
        </div>
      </form>
    </div>
  );
}
