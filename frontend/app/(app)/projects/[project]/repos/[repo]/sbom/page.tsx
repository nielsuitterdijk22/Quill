import Link from "next/link";
import { notFound } from "next/navigation";

import { getCommit, getCommitSBOM, getRepo, getRepoSBOM, getRepoSBOMDocument, getSBOMHistory } from "@/lib/api";
import type { SBOMView } from "@/lib/api";
import { getToken } from "@/lib/session";
import { BrowseError, RepoHeader, firstLine, fmtDate, repoBase, shortSha, treeHref } from "@/components/repo";
import { diffSBOM, sbomRows } from "@/lib/sbom";
import { SBOMExplorer } from "./SBOMExplorer";
import { SBOMDownload } from "./SBOMDownload";
import { SBOMAutoRefresh } from "./SBOMAutoRefresh";
import { SBOMCommitPicker } from "./SBOMCommitPicker";
import { SBOMGenerate } from "./SBOMGenerate";

// SBOM viewer ("Dependencies"): the CycloneDX SBOM of one commit, rendered
// natively. Defaults to the default branch head; ?sha= picks any commit
// (every push to the default branch gets one automatically, the last 30
// are kept, older commits can be generated on request). Shows composition
// charts, where components come from (linking into the code at that
// commit), what changed since the previous SBOM, and a searchable table.

const SHA = /^[0-9a-f]{40}$/;

export default async function SBOMPage({
  params,
  searchParams,
}: {
  params: { project: string; repo: string };
  searchParams: { sha?: string; q?: string };
}) {
  const token = await getToken();
  if (!token) notFound();
  const requested = searchParams.sha && SHA.test(searchParams.sha) ? searchParams.sha : undefined;

  const [repoRes, statusRes, historyRes, commitStatusRes] = await Promise.all([
    getRepo(token, params.project, params.repo),
    getRepoSBOM(token, params.project, params.repo),
    getSBOMHistory(token, params.project, params.repo),
    requested ? getCommitSBOM(token, params.project, params.repo, requested) : Promise.resolve(null),
  ]);
  if (!repoRes.ok) {
    if (repoRes.status === 404) notFound();
    return <BrowseError project={params.project} repo={params.repo} status={repoRes.status} message={repoRes.message} />;
  }
  const repo = repoRes.data;
  const base = repoBase(params.project, params.repo);
  const status = statusRes.ok ? statusRes.data : { sbom: null };
  const history = historyRes.ok ? historyRes.data.sboms : [];
  const head = status.sbom?.commitSha;

  // Which SBOM to show: the requested commit's, else the head's (or the
  // newest ready one while the head's is still generating).
  let view: SBOMView | null;
  if (requested) {
    view = commitStatusRes && commitStatusRes.ok ? commitStatusRes.data.sbom : null;
  } else {
    view = status.sbom?.status === "ready" ? status.sbom : status.latestReady ?? status.sbom;
  }
  const sha = requested ?? view?.commitSha;

  const [docRes, commitRes] = await Promise.all([
    sha && view?.status === "ready" ? getRepoSBOMDocument(token, params.project, params.repo, sha) : Promise.resolve(null),
    sha ? getCommit(token, params.project, params.repo, sha) : Promise.resolve(null),
  ]);
  const commit = commitRes && commitRes.ok ? commitRes.data.commit : null;
  if (requested && commitRes && !commitRes.ok && commitRes.status === 404) notFound();

  // The previous default-branch SBOM to compare against: the next older ready
  // branch-head entry in history.
  const idx = history.findIndex((h) => h.commitSha === sha);
  const prev =
    idx >= 0 && history[idx].ref !== ""
      ? history.slice(idx + 1).find((h) => h.status === "ready" && h.ref !== "")
      : undefined;
  const prevDocRes =
    prev && docRes?.ok ? await getRepoSBOMDocument(token, params.project, params.repo, prev.commitSha) : null;

  const headGenerating = status.sbom && (status.sbom.status === "pending" || status.sbom.status === "running");
  const viewGenerating = view && (view.status === "pending" || view.status === "running");
  const isHead = !!sha && sha === head;

  const header = (
    <RepoHeader project={params.project} repo={params.repo} visibility={repo.visibility} refName={repo.defaultBranch} active="sbom" />
  );

  const hero = (
    <div className="panel sbom-hero">
      <div className="sbom-hero-main">
        <div className="sbom-hero-text">
          <h1>Dependencies</h1>
          {sha ? (
            <div className="run-meta">
              <span>
                <Link href={`${base}/commit/${sha}`} className="mono">
                  {shortSha(sha)}
                </Link>{" "}
                {commit ? <span className="sbom-subject">{firstLine(commit.message)}</span> : null}
              </span>
              {isHead ? <span className="tag">{repo.defaultBranch} head</span> : null}
              {commit && <span>Committed {fmtDate(commit.date)}</span>}
              {docRes?.ok && <span>CycloneDX {docRes.data.specVersion}</span>}
              {view?.finishedAt && view.status === "ready" && <span>Scanned {fmtDate(view.finishedAt)}</span>}
            </div>
          ) : (
            <div className="sub">Software bill of materials: every third-party component in the code.</div>
          )}
        </div>
        {sha && (
          <div className="sbom-hero-actions">
            {history.length > 0 && <SBOMCommitPicker base={base} history={history} current={sha} head={head} />}
            <Link className="btn ghost" href={treeHref(params.project, params.repo, sha)}>
              Browse code at {shortSha(sha)}
            </Link>
            {docRes?.ok && <SBOMDownload project={params.project} repo={params.repo} sha={sha} />}
          </div>
        )}
      </div>
      {!requested && headGenerating && status.sbom && status.sbom.commitSha !== sha && (
        <div className="sbom-note">
          <span className="run-spinner" aria-hidden /> The SBOM for the newest commit{" "}
          <span className="mono">{shortSha(status.sbom.commitSha)}</span> is being generated. This page updates when
          it is ready.
        </div>
      )}
    </div>
  );

  if (!docRes || !docRes.ok) {
    return (
      <>
        {header}
        <SBOMAutoRefresh active={!!(viewGenerating || headGenerating)} />
        {hero}
        <div className="panel">
          {!sha ? (
            <div className="empty">This repository has no commits on {repo.defaultBranch} yet.</div>
          ) : viewGenerating ? (
            <div className="sbom-note">
              <span className="run-spinner" aria-hidden /> Scanning <span className="mono">{shortSha(sha)}</span>…
              this usually takes under a minute.
            </div>
          ) : view?.status === "failed" ? (
            <div className="sbom-missing">
              <div className="banner">The scan failed: {view.error || "unknown error"}</div>
              <SBOMGenerate project={params.project} repo={params.repo} sha={sha} />
            </div>
          ) : (
            <div className="sbom-missing">
              <p>
                There is no SBOM for <span className="mono">{shortSha(sha)}</span>. Every commit pushed to{" "}
                {repo.defaultBranch} is scanned automatically and the last 30 scans are kept; older commits can be
                scanned on request.
              </p>
              <SBOMGenerate project={params.project} repo={params.repo} sha={sha} />
            </div>
          )}
        </div>
      </>
    );
  }

  const rows = sbomRows(docRes.data);
  const changes = prevDocRes?.ok ? diffSBOM(sbomRows(prevDocRes.data), rows) : null;
  const unknown = rows.filter((r) => r.risk === "unknown").length;
  const strong = rows.filter((r) => r.risk === "strong").length;
  const ecosystems = new Set(rows.map((r) => r.ecosystem)).size;
  const counts = changes
    ? {
        added: changes.filter((c) => c.kind === "added").length,
        removed: changes.filter((c) => c.kind === "removed").length,
        updated: changes.filter((c) => c.kind === "updated").length,
      }
    : null;

  return (
    <>
      {header}
      <SBOMAutoRefresh active={!!headGenerating} />
      {hero}
      <div className="sbom-stats">
        <div className="sbom-stat">
          <b>{rows.length}</b>
          <span>components</span>
        </div>
        <div className="sbom-stat">
          <b>{ecosystems}</b>
          <span>ecosystem{ecosystems === 1 ? "" : "s"}</span>
        </div>
        <div className={`sbom-stat${strong ? " bad" : ""}`}>
          <b>{strong}</b>
          <span>strong copyleft</span>
        </div>
        <div className={`sbom-stat${unknown ? " warn" : ""}`}>
          <b>{unknown}</b>
          <span>without license info</span>
        </div>
        {counts && prev && (
          <div className="sbom-stat">
            <b>
              {counts.added + counts.removed + counts.updated}
            </b>
            <span>
              changes since{" "}
              <Link className="mono" href={`${base}/sbom?sha=${prev.commitSha}`}>
                {shortSha(prev.commitSha)}
              </Link>
            </span>
          </div>
        )}
      </div>

      {rows.length === 0 ? (
        <div className="panel">
          <div className="empty">
            No third-party components were found at this commit (no package manifests or lock files).
          </div>
        </div>
      ) : (
        <SBOMExplorer
          rows={rows}
          project={params.project}
          repo={params.repo}
          sha={sha!}
          initialQuery={searchParams.q ?? ""}
        />
      )}

      {changes && prev && (
        <details className="panel sbom-changes" open={changes.length > 0 && changes.length <= 25}>
          <summary>
            <b>Changes since {shortSha(prev.commitSha)}</b>{" "}
            {changes.length === 0 ? (
              <span className="sub">none: same components</span>
            ) : (
              <span className="sbom-change-counts">
                {counts!.updated > 0 && <span className="sbom-chg updated">↑ {counts!.updated} updated</span>}
                {counts!.added > 0 && <span className="sbom-chg added">+ {counts!.added} added</span>}
                {counts!.removed > 0 && <span className="sbom-chg removed">− {counts!.removed} removed</span>}
              </span>
            )}
          </summary>
          {changes.length > 0 && (
            <ul className="sbom-change-list">
              {changes.map((c) => (
                <li key={`${c.kind}-${c.ecosystem}-${c.name}`} className={`sbom-chg-row ${c.kind}`}>
                  <span className={`sbom-chg ${c.kind}`}>{c.kind === "added" ? "+" : c.kind === "removed" ? "−" : "↑"}</span>
                  <span className="mono sbom-name">{c.name}</span>
                  <span className="sub">{c.ecosystem}</span>
                  <span className="mono">
                    {c.kind === "added" ? c.to : c.kind === "removed" ? c.from : `${c.from} → ${c.to}`}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </details>
      )}
    </>
  );
}
