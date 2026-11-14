"use client";

import { useState } from "react";

// Downloads the raw CycloneDX JSON through the app's authenticated proxy.
// (A plain <a href> works too, but fetching first lets us show failures.)
export function SBOMDownload({ project, repo, sha }: { project: string; repo: string; sha: string }) {
  const [state, setState] = useState<"idle" | "busy" | "error">("idle");
  const go = async () => {
    setState("busy");
    try {
      const res = await fetch(
        `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/sbom/download?sha=${sha}`,
        { cache: "no-store" },
      );
      if (!res.ok) throw new Error(String(res.status));
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${repo}-${sha.slice(0, 7)}.cdx.json`;
      a.click();
      URL.revokeObjectURL(url);
      setState("idle");
    } catch {
      setState("error");
    }
  };
  return (
    <div className="sbom-dl">
      <button type="button" className="btn" onClick={go} disabled={state === "busy"}>
        {state === "busy" ? "Preparing…" : "Download CycloneDX JSON"}
      </button>
      {state === "error" && <span className="form-error">Download failed — try again.</span>}
    </div>
  );
}
