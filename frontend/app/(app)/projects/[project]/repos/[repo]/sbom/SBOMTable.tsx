"use client";

import { useMemo, useState } from "react";

export type SBOMRow = {
  name: string;
  version: string;
  ecosystem: string;
  license: string;
  purl: string;
};

type SortKey = "name" | "ecosystem" | "license";

export function SBOMTable({ rows, ecosystems }: { rows: SBOMRow[]; ecosystems: [string, number][] }) {
  const [q, setQ] = useState("");
  const [eco, setEco] = useState<string | null>(null);
  const [sort, setSort] = useState<SortKey>("name");
  const [copied, setCopied] = useState<string | null>(null);

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return rows
      .filter((r) => (!eco || r.ecosystem === eco) &&
        (!needle || r.name.toLowerCase().includes(needle) || r.license.toLowerCase().includes(needle) || r.purl.toLowerCase().includes(needle)))
      .sort((a, b) => (a[sort] || "~").localeCompare(b[sort] || "~") || a.name.localeCompare(b.name));
  }, [rows, q, eco, sort]);

  const copy = async (purl: string) => {
    try {
      await navigator.clipboard.writeText(purl);
      setCopied(purl);
      setTimeout(() => setCopied((c) => (c === purl ? null : c)), 1500);
    } catch {
      /* clipboard unavailable */
    }
  };

  return (
    <>
      <div className="sbom-filters">
        <input
          className="sbom-search"
          type="search"
          placeholder="Filter components, licenses, purls…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          aria-label="Filter components"
        />
        <div className="sbom-eco">
          <button type="button" className={`sbom-chip${eco === null ? " on" : ""}`} onClick={() => setEco(null)}>
            All <b>{rows.length}</b>
          </button>
          {ecosystems.map(([e, n]) => (
            <button type="button" key={e} className={`sbom-chip${eco === e ? " on" : ""}`} onClick={() => setEco(eco === e ? null : e)}>
              {e} <b>{n}</b>
            </button>
          ))}
        </div>
      </div>
      <div className="sbom-table-wrap">
        <table className="sbom-table">
          <thead>
            <tr>
              <th>
                <button type="button" onClick={() => setSort("name")} aria-pressed={sort === "name"}>Component</button>
              </th>
              <th>Version</th>
              <th>
                <button type="button" onClick={() => setSort("ecosystem")} aria-pressed={sort === "ecosystem"}>Ecosystem</button>
              </th>
              <th>
                <button type="button" onClick={() => setSort("license")} aria-pressed={sort === "license"}>License</button>
              </th>
              <th aria-label="Package URL" />
            </tr>
          </thead>
          <tbody>
            {shown.map((r, i) => (
              <tr key={`${r.purl || r.name}-${i}`}>
                <td className="mono sbom-name" title={r.purl || r.name}>{r.name}</td>
                <td className="mono">{r.version || "—"}</td>
                <td><span className="sbom-chip small">{r.ecosystem}</span></td>
                <td className={r.license ? "" : "muted"}>{r.license || "Unknown"}</td>
                <td>
                  {r.purl && (
                    <button type="button" className="btn ghost small" onClick={() => copy(r.purl)} title={r.purl}>
                      {copied === r.purl ? "Copied" : "Copy purl"}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {shown.length === 0 && <div className="empty">No components match.</div>}
      </div>
      <div className="sub sbom-count">
        Showing {shown.length} of {rows.length}
      </div>
    </>
  );
}
