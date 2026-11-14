"use client";

import Link from "next/link";
import { Fragment, useMemo, useState } from "react";

import { blobHref } from "@/components/repo";
import { RISK_HINT, RISK_LABEL, RISK_ORDER, tally, type LicenseRisk, type SBOMRow } from "@/lib/sbom";

// SBOMExplorer: the interactive half of the SBOM viewer. Two composition bars
// (ecosystems, license risk) and the source manifests double as filters for
// the component table below; every row expands into its details with a jump
// to the manifest at this commit and a search across all repositories.

// Ecosystems keep a fixed categorical slot so a colour means the same thing
// on every commit and every repo; anything else folds into "other".
const ECO_SLOT: Record<string, number> = { npm: 1, golang: 2, pypi: 3, maven: 4, cargo: 5, gem: 6, nuget: 7 };
const ecoClass = (eco: string) => `sbom-c${ECO_SLOT[eco] ?? 8}`;

type Segment = { key: string; label: string; count: number; cls: string; hint?: string };

function CompositionBar({
  title,
  segments,
  total,
  active,
  onPick,
}: {
  title: string;
  segments: Segment[];
  total: number;
  active: string | null;
  onPick: (key: string | null) => void;
}) {
  const [hover, setHover] = useState<Segment | null>(null);
  const shown = hover ?? segments.find((s) => s.key === active) ?? null;
  const pct = (n: number) => (total ? Math.round((n / total) * 1000) / 10 : 0);
  return (
    <div className="sbom-comp">
      <div className="sbom-comp-head">
        <span className="sbom-comp-title">{title}</span>
        <span className="sub sbom-comp-readout" aria-live="polite">
          {shown ? (
            <>
              <b>{shown.label}</b> · {shown.count} component{shown.count === 1 ? "" : "s"} ({pct(shown.count)}%)
              {shown.hint ? <> · {shown.hint}</> : null}
            </>
          ) : (
            "Hover a segment for details · click to filter"
          )}
        </span>
      </div>
      <div className="sbom-bar" role="group" aria-label={title} onMouseLeave={() => setHover(null)}>
        {segments.map((s) => (
          <button
            type="button"
            key={s.key}
            className={`sbom-seg ${s.cls}${active && active !== s.key ? " dim" : ""}`}
            style={{ flexGrow: s.count }}
            aria-label={`${s.label}: ${s.count} (${pct(s.count)}%)`}
            aria-pressed={active === s.key}
            onMouseEnter={() => setHover(s)}
            onFocus={() => setHover(s)}
            onBlur={() => setHover(null)}
            onClick={() => onPick(active === s.key ? null : s.key)}
          />
        ))}
      </div>
      <div className="sbom-legend">
        {segments.map((s) => (
          <button
            type="button"
            key={s.key}
            className={`sbom-legend-item${active === s.key ? " on" : ""}`}
            onClick={() => onPick(active === s.key ? null : s.key)}
            onMouseEnter={() => setHover(s)}
            onMouseLeave={() => setHover(null)}
          >
            <span className={`sbom-swatch ${s.cls}`} aria-hidden />
            {s.label} <b>{s.count}</b>
          </button>
        ))}
      </div>
    </div>
  );
}

export function SBOMExplorer({
  rows,
  project,
  repo,
  sha,
  initialQuery = "",
}: {
  rows: SBOMRow[];
  project: string;
  repo: string;
  sha: string;
  initialQuery?: string;
}) {
  const [q, setQ] = useState(initialQuery);
  const [eco, setEco] = useState<string | null>(null);
  const [risk, setRisk] = useState<string | null>(null);
  const [source, setSource] = useState<string | null>(null);
  const [license, setLicense] = useState<string | null>(null);
  const [openRow, setOpenRow] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  const ecoSegs: Segment[] = useMemo(() => {
    const t = tally(rows.map((r) => r.ecosystem));
    const main = t.filter(([e]) => e in ECO_SLOT);
    const rest = t.filter(([e]) => !(e in ECO_SLOT));
    const segs: Segment[] = main.map(([e, n]) => ({ key: e, label: e, count: n, cls: ecoClass(e) }));
    // Unslotted ecosystems: shown individually when there is one, folded otherwise.
    if (rest.length === 1) segs.push({ key: rest[0][0], label: rest[0][0], count: rest[0][1], cls: "sbom-c8" });
    else if (rest.length > 1)
      segs.push({ key: "__other", label: `other (${rest.map(([e]) => e).join(", ")})`, count: rest.reduce((a, [, n]) => a + n, 0), cls: "sbom-c8" });
    return segs;
  }, [rows]);
  const otherEcos = useMemo(() => new Set(rows.map((r) => r.ecosystem).filter((e) => !(e in ECO_SLOT))), [rows]);

  const riskSegs: Segment[] = useMemo(() => {
    const t = new Map(tally(rows.map((r) => r.risk)));
    return RISK_ORDER.filter((k) => t.get(k)).map((k) => ({
      key: k,
      label: RISK_LABEL[k],
      count: t.get(k) ?? 0,
      cls: `sbom-risk-${k}`,
      hint: RISK_HINT[k],
    }));
  }, [rows]);

  const licenses = useMemo(() => tally(rows.filter((r) => r.license).map((r) => r.license)), [rows]);
  const sources = useMemo(() => {
    const m = new Map<string, { count: number; ecos: Set<string> }>();
    for (const r of rows) {
      if (!r.location) continue;
      const e = m.get(r.location) ?? { count: 0, ecos: new Set<string>() };
      e.count++;
      e.ecos.add(r.ecosystem);
      m.set(r.location, e);
    }
    return [...m.entries()].sort((a, b) => b[1].count - a[1].count || a[0].localeCompare(b[0]));
  }, [rows]);

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return rows
      .filter(
        (r) =>
          (!eco || (eco === "__other" ? otherEcos.has(r.ecosystem) : r.ecosystem === eco)) &&
          (!risk || r.risk === risk) &&
          (!source || r.location === source) &&
          (!license || r.license === license) &&
          (!needle ||
            r.name.toLowerCase().includes(needle) ||
            r.version.toLowerCase().includes(needle) ||
            r.license.toLowerCase().includes(needle) ||
            r.purl.toLowerCase().includes(needle)),
      )
      .sort((a, b) => a.name.localeCompare(b.name) || a.version.localeCompare(b.version));
  }, [rows, q, eco, risk, source, license, otherEcos]);

  const filters = [
    eco && { label: `Ecosystem: ${eco === "__other" ? "other" : eco}`, clear: () => setEco(null) },
    risk && { label: RISK_LABEL[risk as LicenseRisk], clear: () => setRisk(null) },
    license && { label: `License: ${license}`, clear: () => setLicense(null) },
    source && { label: `Source: ${source}`, clear: () => setSource(null) },
  ].filter(Boolean) as { label: string; clear: () => void }[];

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
      <div className="panel sbom-overview">
        <CompositionBar title="Ecosystems" segments={ecoSegs} total={rows.length} active={eco} onPick={setEco} />
        <CompositionBar title="License risk" segments={riskSegs} total={rows.length} active={risk} onPick={setRisk} />
        <div className="sbom-overview-cols">
          <div>
            <div className="sbom-comp-title">Licenses</div>
            {licenses.length === 0 ? (
              <div className="sub">No license information in this SBOM.</div>
            ) : (
              <ul className="sbom-rank">
                {licenses.slice(0, 8).map(([l, n]) => (
                  <li key={l}>
                    <button type="button" className={license === l ? "on" : ""} onClick={() => setLicense(license === l ? null : l)}>
                      <span className="sbom-rank-label">{l}</span>
                      <span className="sbom-rank-track" aria-hidden>
                        <span className="sbom-rank-fill" style={{ width: `${(n / licenses[0][1]) * 100}%` }} />
                      </span>
                      <b>{n}</b>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {licenses.length > 8 && <div className="sub">+{licenses.length - 8} more licenses (use the search)</div>}
          </div>
          <div>
            <div className="sbom-comp-title">Where they come from</div>
            {sources.length === 0 ? (
              <div className="sub">The scanner did not record source files.</div>
            ) : (
              <ul className="sbom-sources">
                {sources.slice(0, 8).map(([path, s]) => (
                  <li key={path}>
                    <Link href={blobHref(project, repo, sha, path)} className="mono" title={`Open ${path} at this commit`}>
                      {path}
                    </Link>
                    <span className="sub">{[...s.ecos].join(", ")}</span>
                    <button
                      type="button"
                      className={`sbom-chip small${source === path ? " on" : ""}`}
                      onClick={() => setSource(source === path ? null : path)}
                      title="Show only these components"
                    >
                      {s.count}
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {sources.length > 8 && <div className="sub">+{sources.length - 8} more files</div>}
          </div>
        </div>
      </div>

      <div className="panel">
        <div className="sbom-filters">
          <input
            className="sbom-search"
            type="search"
            placeholder="Filter by name, version, license or purl…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            aria-label="Filter components"
          />
          {filters.map((f) => (
            <button type="button" key={f.label} className="sbom-chip on" onClick={f.clear} title="Remove filter">
              {f.label} ×
            </button>
          ))}
          <span className="spacer" />
          {q.trim() && (
            <Link className="btn ghost small" href={`/sbom?q=${encodeURIComponent(q.trim())}`}>
              Search all repositories
            </Link>
          )}
        </div>
        <div className="sbom-table-wrap">
          <table className="sbom-table">
            <thead>
              <tr>
                <th>Component</th>
                <th>Version</th>
                <th>Ecosystem</th>
                <th>License</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((r) => {
                const isOpen = openRow === r.key;
                return (
                  <Fragment key={r.key}>
                    <tr
                      className={`sbom-row${isOpen ? " open" : ""}`}
                      onClick={() => setOpenRow(isOpen ? null : r.key)}
                      tabIndex={0}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          setOpenRow(isOpen ? null : r.key);
                        }
                      }}
                      aria-expanded={isOpen}
                    >
                      <td className="mono sbom-name">{r.name}</td>
                      <td className="mono">{r.version || "—"}</td>
                      <td>
                        <span className="sbom-eco-cell">
                          <span className={`sbom-swatch ${ecoClass(r.ecosystem)}`} aria-hidden />
                          {r.ecosystem}
                        </span>
                      </td>
                      <td>
                        <span className="sbom-eco-cell">
                          <span className={`sbom-swatch sbom-risk-${r.risk}`} aria-hidden />
                          <span className={r.license ? "" : "muted"}>{r.license || "Unknown"}</span>
                        </span>
                      </td>
                    </tr>
                    {isOpen && (
                      <tr className="sbom-detail">
                        <td colSpan={4}>
                          <dl>
                            <dt>Package URL</dt>
                            <dd className="mono">
                              {r.purl || "—"}{" "}
                              {r.purl && (
                                <button type="button" className="btn ghost small" onClick={() => copy(r.purl)}>
                                  {copied === r.purl ? "Copied" : "Copy"}
                                </button>
                              )}
                            </dd>
                            <dt>License</dt>
                            <dd>
                              {r.license || "Unknown"} · <span className="sub">{RISK_LABEL[r.risk]}: {RISK_HINT[r.risk]}</span>
                            </dd>
                            {r.location && (
                              <>
                                <dt>Found in</dt>
                                <dd>
                                  <Link className="mono" href={blobHref(project, repo, sha, r.location)}>
                                    {r.location}
                                  </Link>
                                </dd>
                              </>
                            )}
                          </dl>
                          <Link className="btn small" href={`/sbom?q=${encodeURIComponent(r.name)}`}>
                            Where else is {r.name} used?
                          </Link>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
          {shown.length === 0 && <div className="empty">No components match.</div>}
        </div>
        <div className="sub sbom-count">
          Showing {shown.length} of {rows.length}
        </div>
      </div>
    </>
  );
}
