"use client";

import { useFormState, useFormStatus } from "react-dom";

import { updateEmailAction, type EmailFormState } from "./actions";

const initial: EmailFormState = {};

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : "Update email"}
    </button>
  );
}

export function EmailForm({ email }: { email: string }) {
  const [state, formAction] = useFormState(updateEmailAction, initial);

  return (
    <div className="panel form-narrow">
      <h2>Email address</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        {state.ok && <div className="form-success">Email updated.</div>}
        <form action={formAction}>
          <label className="field">
            <span>Email</span>
            <input
              name="email"
              type="email"
              defaultValue={email}
              autoComplete="email"
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
