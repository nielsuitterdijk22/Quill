"use client";

import { useEffect, useRef } from "react";
import { useFormState, useFormStatus } from "react-dom";
import { useClerk } from "@clerk/nextjs";

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
  const clerk = useClerk();
  const signedOut = useRef(false);

  // Once the backend has purged the account, sign out of Clerk before leaving.
  // Without this the Clerk session stays live and the next request would
  // re-provision a fresh Quill user, trapping the browser in a redirect loop.
  //
  // The ref guard is essential: useClerk()'s signOut identity is not stable, so
  // depending on it (or re-running while state.ok stays true) would fire signOut
  // on every render — each triggering a navigation/re-render and hammering
  // /sign-in forever. Fire exactly once.
  useEffect(() => {
    if (state.ok && !signedOut.current) {
      signedOut.current = true;
      void clerk.signOut({ redirectUrl: "/sign-in" });
    }
  }, [state.ok, clerk]);

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
