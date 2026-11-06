"use client";

import { useFormState, useFormStatus } from "react-dom";

import { updateProfileAction, updateUsernameAction, type ProfileFormState } from "./actions";

const initial: ProfileFormState = {};

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : "Save changes"}
    </button>
  );
}

// ProfileForm edits the signed-in user's display name and username. An empty
// display name clears it; the app then falls back to the username.
export function ProfileForm({ displayName, username }: { displayName: string; username: string }) {
  const [state, formAction] = useFormState(updateProfileAction, initial);
  const [usernameState, usernameAction] = useFormState(updateUsernameAction, initial);

  return (
    <div className="panel form-narrow">
      <h2>Username</h2>
      <div className="readme-body">
        {usernameState.error && <div className="form-error">{usernameState.error}</div>}
        {usernameState.ok && <div className="form-success">Username updated.</div>}
        <form action={usernameAction}>
          <label className="field">
            <span>Username</span>
            <input
              name="username"
              defaultValue={username}
              placeholder="your-handle"
              maxLength={63}
              pattern="[a-z0-9][a-z0-9._-]*"
            />
          </label>
          <p className="hint">
            Your personal workspace URL and git identity follow this handle.
          </p>
          <div className="form-actions">
            <SaveButton />
          </div>
        </form>
      </div>
      <h2>Display name</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        {state.ok && <div className="form-success">Profile saved.</div>}
        <form action={formAction}>
          <label className="field">
            <span>Display name</span>
            <input
              name="displayName"
              defaultValue={displayName}
              placeholder="Your name"
              maxLength={100}
            />
          </label>
          <p className="hint">
            Shown across Quill. Leave blank to use your username.
          </p>
          <div className="form-actions">
            <SaveButton />
          </div>
        </form>
      </div>
    </div>
  );
}
