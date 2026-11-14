"use client";

import { useRouter } from "next/navigation";

import type { SBOMView } from "@/lib/api";

// Switches the viewer between the commits that have an SBOM.
export function SBOMCommitPicker({
  base,
  history,
  current,
  head,
}: {
  base: string;
  history: SBOMView[];
  current: string;
  head?: string;
}) {
  const router = useRouter();
  const items = history.some((h) => h.commitSha === current)
    ? history
    : [{ commitSha: current, status: "missing" } as unknown as SBOMView, ...history];
  return (
    <label className="sbom-picker">
      <span className="sub">Commit</span>
      <select
        value={current}
        onChange={(e) =>
          router.push(e.target.value === head ? `${base}/sbom` : `${base}/sbom?sha=${e.target.value}`)
        }
      >
        {items.map((h) => (
          <option key={h.commitSha} value={h.commitSha}>
            {h.commitSha.slice(0, 7)}
            {h.commitSha === head ? " · branch head" : ""}
            {h.status === "ready"
              ? ` · ${h.componentCount} components`
              : h.status === "failed"
                ? " · failed"
                : (h.status as string) === "missing"
                  ? " · no SBOM"
                  : " · generating"}
            {h.createdAt ? ` · ${new Date(h.createdAt).toLocaleDateString()}` : ""}
          </option>
        ))}
      </select>
    </label>
  );
}
