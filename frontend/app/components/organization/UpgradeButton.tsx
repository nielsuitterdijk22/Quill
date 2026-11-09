"use client";

import { useState } from "react";

// UpgradeButton starts a Creem checkout (Team for orgs, Pro for personal
// accounts). On a server where payments aren't activated yet, the truthful 501
// is surfaced as honest copy instead of a dead button.
export function UpgradeButton({
  endpoint,
  label = "Upgrade to Team",
}: {
  endpoint: string;
  label?: string;
}) {
  const [pending, setPending] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  async function startCheckout() {
    setPending(true);
    setNotice(null);
    try {
      const res = await fetch(endpoint, { method: "POST" });
      const body = (await res.json().catch(() => null)) as { url?: string; message?: string } | null;
      if (res.ok && body?.url) {
        window.location.href = body.url;
        return;
      }
      setNotice(body?.message ?? "Could not start the upgrade right now. Please try again.");
    } catch {
      setNotice("Could not start the upgrade right now. Please try again.");
    } finally {
      setPending(false);
    }
  }

  return (
    <div>
      <button className="pill" onClick={startCheckout} disabled={pending}>
        {pending ? "Opening checkout…" : label}
      </button>
      {notice && <p className="subtle" style={{ marginTop: 8 }}>{notice}</p>}
    </div>
  );
}
