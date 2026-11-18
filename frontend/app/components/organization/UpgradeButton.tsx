"use client";

import { useEffect } from "react";
import { useFormState, useFormStatus } from "react-dom";

import { startUpgradeAction } from "../../lib/actions";

function GoldSubmit({ label }: { label: string }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn gold" type="submit" disabled={pending}>
      {pending ? "Opening checkout…" : label}
    </button>
  );
}

// UpgradeButton starts a Creem checkout (Team Pro for orgs, Starter/Pro for
// personal accounts) through a server action that attaches the session bearer
// server-side, so it works regardless of the browser-side proxy. On
// success the browser is redirected to Creem's hosted checkout.
export function UpgradeButton({
  endpoint,
  label = "Subscribe to Team Pro",
  plan,
}: {
  endpoint: string;
  label?: string;
  plan?: string;
}) {
  const [state, action] = useFormState(startUpgradeAction, {});
  useEffect(() => {
    if (state.url) window.location.href = state.url;
  }, [state.url]);

  return (
    <form action={action}>
      <input type="hidden" name="endpoint" value={endpoint} />
      {plan && <input type="hidden" name="plan" value={plan} />}
      <GoldSubmit label={label} />
      {state.error && (
        <p className="subtle" style={{ marginTop: 8 }}>{state.error}</p>
      )}
    </form>
  );
}
