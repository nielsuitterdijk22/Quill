"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuillAuth } from "@/components/auth/context";

// SBOM status + download for the repo landing page. The platform generates a
// CycloneDX SBOM on every push to the default branch (not a pipeline); this
// button shows its state and, when ready, links straight to the document.

type SBOMView = {
  status: "pending" | "running" | "ready" | "failed";
  commitSha: string;
  ref: string;
  componentCount: number;
  specVersion?: string;
  error?: string;
  finishedAt?: string;
};

type State =
  | { kind: "loading" }
  | { kind: "hidden" } // not available for this repo/viewer
  | { kind: "none" }
  | { kind: "sbom"; sbom: SBOMView };

export function SBOMButton({ project, repo }: { project: string; repo: string }) {
  const { getToken } = useQuillAuth();
  const [state, setState] = useState<State>({ kind: "loading" });
  const [busy, setBusy] = useState(false);
  const base = `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/sbom`;

  const load = useCallback(async () => {
    const token = await getToken();
    const res = await fetch(base, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      cache: "no-store",
    });
    if (!res.ok) {
      setState({ kind: "hidden" });
      return;
    }
    const body = (await res.json()) as { sbom: SBOMView | null };
    setState(body.sbom ? { kind: "sbom", sbom: body.sbom } : { kind: "none" });
  }, [base, getToken]);

  useEffect(() => {
    void load();
  }, [load]);

  // Poll while a generation is in flight.
  const inFlight =
    state.kind === "sbom" && (state.sbom.status === "pending" || state.sbom.status === "running");
  useEffect(() => {
    if (!inFlight) return;
    const t = setInterval(() => void load(), 4000);
    return () => clearInterval(t);
  }, [inFlight, load]);

  async function generate() {
    setBusy(true);
    try {
      const token = await getToken();
      const res = await fetch(base, {
        method: "POST",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (res.ok) {
        const body = (await res.json()) as { sbom: SBOMView };
        setState({ kind: "sbom", sbom: body.sbom });
      }
    } finally {
      setBusy(false);
    }
  }

  if (state.kind === "loading" || state.kind === "hidden") return null;

  if (state.kind === "none") {
    return (
      <button type="button" className="pill sbom-btn" onClick={generate} disabled={busy}>
        <span className="sbom-dot" aria-hidden /> Generate SBOM
      </button>
    );
  }

  const { sbom } = state;
  const short = sbom.commitSha.slice(0, 7);
  switch (sbom.status) {
    case "ready":
      return (
        <a
          className="pill sbom-btn sbom-ready"
          href={`${base}/download?sha=${sbom.commitSha}`}
          download
          title={`CycloneDX ${sbom.specVersion ?? ""} SBOM for ${short} · ${sbom.componentCount} components`}
        >
          <span className="sbom-dot" aria-hidden /> SBOM <b>{sbom.componentCount}</b>
        </a>
      );
    case "failed":
      return (
        <button
          type="button"
          className="pill sbom-btn sbom-failed"
          onClick={generate}
          disabled={busy}
          title={sbom.error ? `SBOM for ${short} failed: ${sbom.error}` : `SBOM for ${short} failed`}
        >
          <span className="sbom-dot" aria-hidden /> SBOM failed · retry
        </button>
      );
    default:
      return (
        <span className="pill sbom-btn sbom-pending" title={`Generating the SBOM for ${short}`}>
          <span className="sbom-dot" aria-hidden /> SBOM generating…
        </span>
      );
  }
}
