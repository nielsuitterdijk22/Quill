"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

// Generates the SBOM of an older commit on request (members only; the
// branch head always has one already).
export function SBOMGenerate({ project, repo, sha }: { project: string; repo: string; sha: string }) {
  const router = useRouter();
  const [state, setState] = useState<"idle" | "busy" | "error">("idle");
  const [msg, setMsg] = useState("");
  const go = async () => {
    setState("busy");
    try {
      const res = await fetch(
        `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/sbom`,
        { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ sha }) },
      );
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        throw new Error(body?.message || `HTTP ${res.status}`);
      }
      router.refresh();
      setState("idle");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "failed");
      setState("error");
    }
  };
  return (
    <span className="sbom-dl">
      <button type="button" className="btn" onClick={go} disabled={state === "busy"}>
        {state === "busy" ? "Starting…" : "Generate SBOM for this commit"}
      </button>
      {state === "error" && <span className="form-error">Could not start: {msg}</span>}
    </span>
  );
}
