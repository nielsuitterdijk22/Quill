"use client";

import { useState } from "react";

// ExportDataButton downloads the user's personal data as a JSON file.
// It uses fetch so the httpOnly cookie is sent automatically.
export function ExportDataButton() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/backend/auth/me/export", {
        credentials: "include",
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          message?: string;
        } | null;
        setError(body?.message || `Export failed (${res.status}).`);
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "quill-export.json";
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      setError("Could not reach the backend.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="panel form-narrow">
      <h2>Export your data</h2>
      <div className="readme-body">
        <p className="subtle">
          Download a JSON file of your profile, project memberships, and git
          token metadata (GDPR Article 20 portability).
        </p>
        {error && <div className="form-error">{error}</div>}
        <div className="form-actions">
          <button className="btn" onClick={handleClick} disabled={busy}>
            {busy ? "Preparing…" : "Download export"}
          </button>
        </div>
      </div>
    </div>
  );
}
