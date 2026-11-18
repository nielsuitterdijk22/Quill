"use client";

import { useFormState, useFormStatus } from "react-dom";

import { createBetaInviteAction, type CreateBetaState } from "./actions";

const initial: CreateBetaState = {};

function CreateButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Creating…" : "Create invite link"}
    </button>
  );
}

export function BetaInviteForm() {
  const [state, formAction] = useFormState(createBetaInviteAction, initial);
  return (
    <div className="panel form-narrow">
      <h2>New beta invite</h2>
      <div className="readme-body">
        <p className="subtle">
          Whoever opens the link while signed in gets the free beta plan (Pro limits, no charge). The link
          expires and stops working after its uses are spent.
        </p>
        {state.error && <div className="form-error">{state.error}</div>}
        {state.link && (
          <div className="form-success">
            Copy it now — it is not shown again:
            <br />
            <code className="mono">{state.link}</code>
          </div>
        )}
        <form action={formAction}>
          <label className="field">
            <span>Who is it for?</span>
            <input name="note" type="text" maxLength={120} placeholder="e.g. Sam from Acme" />
          </label>
          <label className="field">
            <span>Uses (accounts that may redeem it)</span>
            <input name="maxUses" type="number" min={1} max={100} defaultValue={1} required />
          </label>
          <label className="field">
            <span>Valid for (days)</span>
            <input name="days" type="number" min={1} max={90} defaultValue={14} required />
          </label>
          <CreateButton />
        </form>
      </div>
    </div>
  );
}
