import Link from "next/link";
import { redirect } from "next/navigation";

import { searchSBOMs } from "@/lib/api";
import type { SBOMPackage } from "@/lib/api";
import { getToken } from "@/lib/session";
import { shortSha } from "@/components/repo";
import { RISK_LABEL, licenseRisk } from "@/lib/sbom";

// Dependencies across repositories: searches the current SBOM (default
// branch head) of every repository in the projects you belong to. Without a
// query it lists the most widely used packages. Each hit links into that
// repo's SBOM viewer at the scanned commit, filtered to the package.

export default async function SBOMSearchPage({ searchParams }: { searchParams: { q?: string } }) {
  const token = await getToken();
  if (!token) redirect("/sign-in");
  const q = (searchParams.q ?? "").trim().slice(0, 200);
  const res = await searchSBOMs(token, q);

  return (
    <>
      <div className="top">
        <h1>Dependencies</h1>
      </div>
      <p className="subtle">
        Search the components of every repository you can access, as of each repository&apos;s latest scan of its
        default branch.
      </p>

      <form className="panel sbom-global-search" action="/sbom" method="get" role="search">
        <input
          className="sbom-search"
          type="search"
          name="q"
          defaultValue={q}
          placeholder="Package name, purl or license (e.g. lodash, golang.org/x/net, GPL-3.0)"
          aria-label="Search dependencies"
          autoFocus
        />
        <button type="submit" className="btn">
          Search
        </button>
      </form>

      {!res.ok ? (
        <div className="banner">Search failed: {res.message}</div>
      ) : (
        <>
          <div className="sub sbom-count">
            {res.data.reposSearched === 0
              ? "None of your repositories has an SBOM yet. Each push to a default branch creates one."
              : q
                ? `${res.data.packages.length}${res.data.truncated ? "+" : ""} package${res.data.packages.length === 1 ? "" : "s"} matching “${q}” across ${res.data.reposSearched} repositor${res.data.reposSearched === 1 ? "y" : "ies"}`
                : `Most used packages across ${res.data.reposSearched} repositor${res.data.reposSearched === 1 ? "y" : "ies"}`}
          </div>
          {res.data.packages.length === 0 && res.data.reposSearched > 0 ? (
            <div className="panel">
              <div className="empty">No component matches “{q}”.</div>
            </div>
          ) : (
            <div className="sbom-results">
              {res.data.packages.map((p) => (
                <PackageCard key={`${p.ecosystem}/${p.name}`} pkg={p} />
              ))}
            </div>
          )}
        </>
      )}
    </>
  );
}

function PackageCard({ pkg }: { pkg: SBOMPackage }) {
  const spread = pkg.versions.length;
  return (
    <div className="panel sbom-pkg">
      <div className="sbom-pkg-head">
        <span className="mono sbom-pkg-name">{pkg.name}</span>
        <span className="sbom-chip small">{pkg.ecosystem}</span>
        <span className="spacer" />
        <span className="sub">
          {pkg.repoCount} repositor{pkg.repoCount === 1 ? "y" : "ies"}
        </span>
        {spread > 1 && (
          <span className="sbom-chip small warn" title="Different repositories use different versions">
            {spread} versions
          </span>
        )}
      </div>
      <table className="sbom-table sbom-pkg-table">
        <tbody>
          {pkg.versions.map((v) => {
            const risk = licenseRisk(v.license);
            return (
              <tr key={v.version}>
                <td className="mono sbom-pkg-ver">{v.version || "—"}</td>
                <td>
                  <span className="sbom-eco-cell" title={RISK_LABEL[risk]}>
                    <span className={`sbom-swatch sbom-risk-${risk}`} aria-hidden />
                    <span className={v.license ? "" : "muted"}>{v.license || "Unknown"}</span>
                  </span>
                </td>
                <td className="sbom-pkg-repos">
                  {v.repos.map((r) => (
                    <Link
                      key={`${r.project}/${r.repo}`}
                      className="sbom-repo-link"
                      href={`/${encodeURIComponent(r.project)}/${encodeURIComponent(r.repo)}/sbom?q=${encodeURIComponent(pkg.name)}`}
                      title={`Scanned at ${shortSha(r.commitSha)}`}
                    >
                      {r.project}/<b>{r.repo}</b>
                    </Link>
                  ))}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
