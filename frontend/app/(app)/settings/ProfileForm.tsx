"use client";

import { useFormState, useFormStatus } from "react-dom";

import { updateProfileAction, type ProfileFormState } from "./actions";

const initial: ProfileFormState = {};

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : "Save changes"}
    </button>
  );
}

// ProfileForm edits the signed-in user's display name. An empty value clears it;
// the app then falls back to the username.
export function ProfileForm({ displayName }: { displayName: string }) {
  const [state, formAction] = useFormState(updateProfileAction, initial);

  return (
    <div className="panel form-narrow">
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
