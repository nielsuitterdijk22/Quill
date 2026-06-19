"use client";

import { useFormState, useFormStatus } from "react-dom";

import { deleteAccountAction, type DeleteAccountState } from "./actions";

const initial: DeleteAccountState = {};

function DeleteButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn danger" type="submit" disabled={pending}>
      {pending ? "Deleting…" : "Delete my account"}
    </button>
  );
}

export function AccountDangerZone() {
  const [state, formAction] = useFormState(deleteAccountAction, initial);

  return (
    <div className="panel danger-zone">
      <h2 className="danger">Delete account</h2>
      <div className="readme-body">
        <p className="subtle">
          This permanently deletes your account, all project memberships, and
          git tokens. Your Forgejo account and any repositories you own will
          also be removed. <strong>This cannot be undone.</strong>
        </p>
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction}>
          <label className="field">
            <span>
              Type <span className="mono">delete my account</span> to confirm
            </span>
            <input
              name="confirm"
              autoComplete="off"
              placeholder="delete my account"
              required
            />
          </label>
          <div className="form-actions">
            <DeleteButton />
          </div>
        </form>
      </div>
    </div>
  );
}
