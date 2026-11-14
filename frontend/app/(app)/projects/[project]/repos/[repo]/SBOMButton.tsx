"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useQuillAuth } from "@/components/auth/context";
import type { SBOMStatus, SBOMView } from "@/lib/api";

// SBOM status on the repo landing page. The platform keeps an SBOM for the
// default branch head at all times (generated on push, backfilled nightly,
// ensured on read), so this is a status + a link to the native viewer —
// never a "generate" button.

export function SBOMButton({ project, repo }: { project: string; repo: string }) {
  const { getToken } = useQuillAuth();
  const [st, setSt] = useState<SBOMStatus | null | "hidden">(null);
  const [busy, setBusy] = useState(false);
  const api = `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/sbom`;
  const viewer = `/${encodeURIComponent(project)}/${encodeURIComponent(repo)}/sbom`;

  const load = useCallback(async () => {
    const token = await getToken();
    const res = await fetch(api, { headers: token ? { Authorization: `Bearer ${token}` } : {}, cache: "no-store" });
    if (!res.ok) {
      setSt("hidden");
      return;
    }
    setSt((await res.json()) as SBOMStatus);
  }, [api, getToken]);

  useEffect(() => {
    void load();
  }, [load]);

  const cur: SBOMView | null = st && st !== "hidden" ? st.sbom : null;
  const ready = st && st !== "hidden" ? (cur?.status === "ready" ? cur : st.latestReady ?? null) : null;
  const inFlight = cur?.status === "pending" || cur?.status === "running";
  useEffect(() => {
    if (!inFlight) return;
    const t = setInterval(() => void load(), 4000);
    return () => clearInterval(t);
  }, [inFlight, load]);

  async function retry() {
    setBusy(true);
    try {
      const token = await getToken();
      await fetch(api, { method: "POST", headers: token ? { Authorization: `Bearer ${token}` } : {} });
      await load();
    } finally {
      setBusy(false);
    }
  }

  if (!st || st === "hidden" || !cur) return null;

  if (cur.status === "failed" && !ready) {
    return (
      <button
        type="button"
        className="pill sbom-btn sbom-failed"
        onClick={retry}
        disabled={busy}
        title={cur.error ? `SBOM failed: ${cur.error}` : "SBOM failed"}
      >
        <span className="sbom-dot" aria-hidden /> SBOM failed · retry
      </button>
    );
  }
  if (!ready) {
    return (
      <span className="pill sbom-btn sbom-pending" title={`Generating the SBOM for ${cur.commitSha.slice(0, 7)}`}>
        <span className="sbom-dot" aria-hidden /> SBOM generating…
      </span>
    );
  }
  const stale = ready.commitSha !== cur.commitSha;
  return (
    <Link
      className={`pill sbom-btn ${stale ? "sbom-pending" : "sbom-ready"}`}
      href={viewer}
      title={
        stale
          ? `SBOM for ${ready.commitSha.slice(0, 7)} (${ready.componentCount} components) · newer one ${cur.status === "failed" ? "failed" : "generating"}`
          : `CycloneDX ${ready.specVersion ?? ""} SBOM for ${ready.commitSha.slice(0, 7)} · ${ready.componentCount} components`
      }
    >
      <span className="sbom-dot" aria-hidden /> SBOM <b>{ready.componentCount}</b>
    </Link>
  );
}
