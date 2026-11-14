import Link from "next/link";
import { notFound } from "next/navigation";

import { getRepo, getRepoSBOM, getRepoSBOMDocument } from "@/lib/api";
import type { CycloneDXComponent } from "@/lib/api";
import { getToken } from "@/lib/session";
import { BrowseError, RepoHeader, fmtDate, repoBase, shortSha } from "@/components/repo";
import { SBOMTable, type SBOMRow } from "./SBOMTable";
import { SBOMDownload } from "./SBOMDownload";
import { SBOMAutoRefresh } from "./SBOMAutoRefresh";

// SBOM viewer (2026-11-14): the repo's CycloneDX SBOM rendered natively —
// summary, ecosystem and license breakdowns, and a searchable component
// table — with the raw document one click away.

function ecosystem(c: CycloneDXComponent): string {
  const m = /^pkg:([^/]+)\//.exec(c.purl ?? "");
  if (m) return m[1];
  return c.type ?? "other";
}

function licenseOf(c: CycloneDXComponent): string {
  const ls = (c.licenses ?? [])
    .map((l) => l.expression ?? l.license?.id ?? l.license?.name ?? "")
    .filter(Boolean);
  return ls.join(" OR ");
}

function tally(values: string[]): [string, number][] {
  const m = new Map<string, number>();
  for (const v of values) m.set(v, (m.get(v) ?? 0) + 1);
  return [...m.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

export default async function SBOMPage({
  params,
  searchParams,
}: {
  params: { project: string; repo: string };
  searchParams: { sha?: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const [repoRes, statusRes] = await Promise.all([
    getRepo(token, params.project, params.repo),
    getRepoSBOM(token, params.project, params.repo),
  ]);
  if (!repoRes.ok) {
    if (repoRes.status === 404) notFound();
    return <BrowseError project={params.project} repo={params.repo} status={repoRes.status} message={repoRes.message} />;
  }
  const repo = repoRes.data;
  const status = statusRes.ok ? statusRes.data : { sbom: null };
  const current = status.sbom;
  const shown =
    searchParams.sha && /^[0-9a-f]{40}$/.test(searchParams.sha)
      ? searchParams.sha
      : current?.status === "ready"
        ? current.commitSha
        : status.latestReady?.commitSha;
  const shownView = current?.commitSha === shown ? current : status.latestReady;
  const docRes = shown ? await getRepoSBOMDocument(token, params.project, params.repo, shown) : null;
  const generating = current && (current.status === "pending" || current.status === "running");

  const header = (
    <RepoHeader
      project={params.project}
      repo={params.repo}
      visibility={repo.visibility}
      refName={repo.defaultBranch}
      active="code"
    />
  );

  if (!docRes || !docRes.ok) {
    return (
      <>
        {header}
        <SBOMAutoRefresh active={!!generating} />
        <div className="panel sbom-hero">
          <h1>Software Bill of Materials</h1>
          {generating ? (
            <div className="sbom-note">
              <span className="run-spinner" aria-hidden /> Generating the SBOM for{" "}
              <span className="mono">{shortSha(current!.commitSha)}</span>. This page updates when it is ready.
            </div>
          ) : current?.status === "failed" ? (
            <div className="banner">SBOM generation failed: {current.error || "unknown error"}</div>
          ) : (
            <div className="empty">This repository has no commits on {repo.defaultBranch} yet.</div>
          )}
        </div>
      </>
    );
  }

  const doc = docRes.data;
  const components = doc.components ?? [];
  const rows: SBOMRow[] = components.map((c) => ({
    name: c.group ? `${c.group}/${c.name}` : c.name,
    version: c.version ?? "",
    ecosystem: ecosystem(c),
    license: licenseOf(c),
    purl: c.purl ?? "",
  }));
  const ecosystems = tally(rows.map((r) => r.ecosystem));
  const licenses = tally(rows.map((r) => r.license || "Unknown"));
  const unknownLicenses = rows.filter((r) => !r.license).length;
  const base = repoBase(params.project, params.repo);

  return (
    <>
      {header}
      <SBOMAutoRefresh active={!!generating} />
      <div className="panel sbom-hero">
        <div className="sbom-hero-main">
          <div>
            <h1>Software Bill of Materials</h1>
            <div className="run-meta">
              <span>
                <Link href={`${base}/commit/${shown}`} className="mono">
                  {shortSha(shown!)}
                </Link>{" "}
                on {repo.defaultBranch}
              </span>
              <span>CycloneDX {doc.specVersion}</span>
              {shownView?.finishedAt && <span>Generated {fmtDate(shownView.finishedAt)}</span>}
            </div>
          </div>
          <SBOMDownload project={params.project} repo={params.repo} sha={shown!} />
        </div>
        {generating && current.commitSha !== shown && (
          <div className="sbom-note">
            <span className="run-spinner" aria-hidden /> A newer SBOM for{" "}
            <span className="mono">{shortSha(current.commitSha)}</span> is being generated.
          </div>
        )}
        <div className="sbom-stats">
          <div className="sbom-stat">
            <b>{rows.length}</b>
            <span>components</span>
          </div>
          <div className="sbom-stat">
            <b>{ecosystems.length}</b>
            <span>ecosystem{ecosystems.length === 1 ? "" : "s"}</span>
          </div>
          <div className="sbom-stat">
            <b>{licenses.filter(([l]) => l !== "Unknown").length}</b>
            <span>distinct licenses</span>
          </div>
          <div className={`sbom-stat${unknownLicenses ? " warn" : ""}`}>
            <b>{unknownLicenses}</b>
            <span>without license info</span>
          </div>
        </div>
        {licenses.length > 0 && (
          <div className="sbom-breakdown">
            <span className="sub">Licenses</span>
            {licenses.slice(0, 10).map(([l, n]) => (
              <span className={`sbom-chip${l === "Unknown" ? " muted" : ""}`} key={l}>
                {l} <b>{n}</b>
              </span>
            ))}
            {licenses.length > 10 && <span className="sub">+{licenses.length - 10} more</span>}
          </div>
        )}
      </div>

      <div className="panel">
        {rows.length === 0 ? (
          <div className="empty">
            No third-party components were detected at this commit (no package manifests or lock files).
          </div>
        ) : (
          <SBOMTable rows={rows} ecosystems={ecosystems} />
        )}
      </div>
    </>
  );
}
