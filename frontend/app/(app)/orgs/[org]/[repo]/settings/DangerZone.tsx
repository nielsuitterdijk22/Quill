"use client";

import { useFormState, useFormStatus } from "react-dom";

import {
  deleteRepoAction,
  renameRepoAction,
  setRepoArchivedAction,
  type RepoSettingsFormState,
} from "./actions";

const initial: RepoSettingsFormState = {};

function PendingButton({
  idle,
  busy,
  disabled,
}: {
  idle: string;
  busy: string;
  disabled?: boolean;
}) {
  const { pending } = useFormStatus();
  return (
    <button
      className="btn danger"
      type="submit"
      disabled={pending || disabled}
    >
      {pending ? busy : idle}
    </button>
  );
}

// RenameRow changes the repository slug. On success the action redirects to the
// settings page at the new slug, so no inline success state is needed.
function RenameRow({ org, repo }: { org: string; repo: string }) {
  const action = renameRepoAction.bind(null, org, repo);
  const [state, formAction] = useFormState(action, initial);
  return (
    <div className="danger-row">
      <div className="danger-row-text">
        <strong>Rename repository</strong>
        <span className="subtle">
          Changes the repository&rsquo;s URL and its Forgejo git repository.
        </span>
      </div>
      <form action={formAction} className="danger-form">
        {state.error && <div className="form-error">{state.error}</div>}
        <div className="danger-controls">
          <input
            name="slug"
            defaultValue={repo}
            pattern="[A-Za-z0-9._\-]+"
            aria-label="New repository name"
            required
          />
          <PendingButton idle="Rename" busy="Renaming…" />
        </div>
      </form>
    </div>
  );
}

// ArchiveRow flips the archived flag. The desired next state is bound into the
// action so this single button both archives and unarchives.
function ArchiveRow({
  org,
  repo,
  archived,
}: {
  org: string;
  repo: string;
  archived: boolean;
}) {
  const action = setRepoArchivedAction.bind(null, org, repo, !archived);
  const [state, formAction] = useFormState(action, initial);
  return (
    <div className="danger-row">
      <div className="danger-row-text">
        <strong>{archived ? "Unarchive repository" : "Archive repository"}</strong>
        <span className="subtle">
          {archived
            ? "Restore write access and mark the repository active again."
            : "Make the repository read-only without deleting its history."}
        </span>
      </div>
      <form action={formAction} className="danger-form">
        {state.error && <div className="form-error">{state.error}</div>}
        <div className="danger-controls">
          <PendingButton
            idle={archived ? "Unarchive" : "Archive"}
            busy="Working…"
          />
        </div>
      </form>
    </div>
  );
}

// DeleteRow permanently removes the repository. The user must retype the slug to
// arm the button; the action redirects to the org overview on success.
function DeleteRow({ org, repo }: { org: string; repo: string }) {
  const action = deleteRepoAction.bind(null, org, repo);
  const [state, formAction] = useFormState(action, initial);
  return (
    <div className="danger-row">
      <div className="danger-row-text">
        <strong>Delete repository</strong>
        <span className="subtle">
          Permanently removes the repository, its git history, and its policies.
          This cannot be undone.
        </span>
      </div>
      <form action={formAction} className="danger-form">
        {state.error && <div className="form-error">{state.error}</div>}
        <div className="danger-controls">
          <input
            name="confirm"
            placeholder={`Type "${repo}" to confirm`}
            aria-label="Confirm repository name"
            autoComplete="off"
            required
          />
          <PendingButton idle="Delete" busy="Deleting…" />
        </div>
      </form>
    </div>
  );
}

// DangerZone groups the irreversible repository operations: rename, archive, and
// delete. Each row is an independent form with its own validation state.
export function DangerZone({
  org,
  repo,
  archived,
}: {
  org: string;
  repo: string;
  archived: boolean;
}) {
  return (
    <div className="panel danger-zone">
      <RenameRow org={org} repo={repo} />
      <ArchiveRow org={org} repo={repo} archived={archived} />
      <DeleteRow org={org} repo={repo} />
    </div>
  );
}
