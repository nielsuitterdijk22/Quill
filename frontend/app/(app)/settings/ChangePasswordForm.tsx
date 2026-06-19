"use client";

import { useFormState, useFormStatus } from "react-dom";

import { changePasswordAction, type PasswordFormState } from "./actions";

const initial: PasswordFormState = {};

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Updating…" : "Update password"}
    </button>
  );
}

export function ChangePasswordForm() {
  const [state, formAction] = useFormState(changePasswordAction, initial);

  return (
    <div className="panel form-narrow">
      <h2>Change password</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        {state.ok && <div className="form-success">Password updated.</div>}
        <form action={formAction}>
          <label className="field">
            <span>Current password</span>
            <input
              name="currentPassword"
              type="password"
              autoComplete="current-password"
              required
            />
          </label>
          <label className="field">
            <span>New password</span>
            <input
              name="newPassword"
              type="password"
              autoComplete="new-password"
              minLength={8}
              required
            />
          </label>
          <label className="field">
            <span>Confirm new password</span>
            <input
              name="confirmPassword"
              type="password"
              autoComplete="new-password"
              minLength={8}
              required
            />
          </label>
          <div className="form-actions">
            <SaveButton />
          </div>
        </form>
      </div>
    </div>
  );
}
