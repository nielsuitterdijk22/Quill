// SBOM helpers shared by the repo SBOM viewer and the cross-repo search:
// flattening CycloneDX components into rows, license risk classes, and the
// diff between two commits' SBOMs.

import type { CycloneDXComponent, CycloneDXDocument } from "./api/types";

export type SBOMRow = {
  key: string;
  name: string;
  version: string;
  ecosystem: string;
  license: string;
  risk: LicenseRisk;
  purl: string;
  location: string; // manifest/lock file the component was found in
};

export type LicenseRisk = "permissive" | "weak" | "strong" | "other" | "unknown";

export const RISK_ORDER: LicenseRisk[] = ["permissive", "weak", "strong", "other", "unknown"];

export const RISK_LABEL: Record<LicenseRisk, string> = {
  permissive: "Permissive",
  weak: "Weak copyleft",
  strong: "Strong copyleft",
  other: "Other license",
  unknown: "No license info",
};

export const RISK_HINT: Record<LicenseRisk, string> = {
  permissive: "MIT, BSD, Apache, ISC and similar: use freely with attribution.",
  weak: "LGPL, MPL, EPL and similar: changes to the component itself must be shared.",
  strong: "GPL, AGPL and similar: can oblige you to release your own code. Review these.",
  other: "A named license Quill does not classify. Check its terms.",
  unknown: "The scanner found no license for these components (common for Go modules).",
};

export function ecosystemOf(c: CycloneDXComponent): string {
  const m = /^pkg:([^/]+)\//.exec(c.purl ?? "");
  if (m) return m[1];
  return c.type ?? "other";
}

export function licenseOf(c: CycloneDXComponent): string {
  return (c.licenses ?? [])
    .map((l) => l.expression ?? l.license?.id ?? l.license?.name ?? "")
    .filter(Boolean)
    .join(" OR ");
}

const STRONG = /\b(A?GPL|SSPL|EUPL|OSL|CPAL|RPL)\b/i;
const WEAK = /\b(LGPL|MPL|EPL|CDDL|CPL|MS-RL|LPL)\b/i;
const PERMISSIVE =
  /\b(MIT|MIT-0|X11|BSD|0BSD|Apache|ISC|Unlicense|CC0|Zlib|BSL-1\.0|Boost|PSF|Python|WTFPL|BlueOak|Artistic|Ruby|PostgreSQL|NCSA|UPL|CC-BY-[0-9]|OpenSSL|libpng|HPND|curl)\b/i;

function riskOfOne(lic: string): LicenseRisk {
  // LGPL contains "GPL": test the weak family first.
  if (WEAK.test(lic)) return "weak";
  if (STRONG.test(lic)) return "strong";
  if (PERMISSIVE.test(lic)) return "permissive";
  return "other";
}

const RANK: Record<LicenseRisk, number> = { permissive: 0, weak: 1, strong: 2, other: 3, unknown: 4 };

// licenseRisk classifies an SPDX-ish expression: for "A OR B" you may pick the
// most permissive choice; for "A AND B" the most restrictive one applies.
export function licenseRisk(license: string): LicenseRisk {
  const lic = license.trim();
  if (!lic) return "unknown";
  const alternatives = lic.split(/\s+OR\s+/i);
  let best: LicenseRisk | null = null;
  for (const alt of alternatives) {
    let worst: LicenseRisk = "permissive";
    for (const part of alt.replace(/[()]/g, " ").split(/\s+AND\s+|\s+WITH\s+/i)) {
      const r = riskOfOne(part);
      if (RANK[r] > RANK[worst]) worst = r;
    }
    if (best === null || RANK[worst] < RANK[best]) best = worst;
  }
  return best ?? "other";
}

export function locationOf(c: CycloneDXComponent): string {
  const p = (c.properties ?? []).find((x) => x.name === "syft:location:0:path");
  return (p?.value ?? "").replace(/^\/+/, "");
}

export function sbomRows(doc: CycloneDXDocument): SBOMRow[] {
  return (doc.components ?? [])
    .filter((c) => c.name)
    .map((c, i) => {
      const license = licenseOf(c);
      return {
        key: `${c["bom-ref"] ?? c.purl ?? c.name}-${i}`,
        name: c.group ? `${c.group}/${c.name}` : c.name,
        version: c.version ?? "",
        ecosystem: ecosystemOf(c),
        license,
        risk: licenseRisk(license),
        purl: c.purl ?? "",
        location: locationOf(c),
      };
    });
}

export function tally(values: string[]): [string, number][] {
  const m = new Map<string, number>();
  for (const v of values) m.set(v, (m.get(v) ?? 0) + 1);
  return [...m.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

export type SBOMChange =
  | { kind: "added"; name: string; ecosystem: string; to: string }
  | { kind: "removed"; name: string; ecosystem: string; from: string }
  | { kind: "updated"; name: string; ecosystem: string; from: string; to: string };

// diffSBOM compares two commits' components by (ecosystem, name). A package
// present in several versions compares as its sorted version list.
export function diffSBOM(before: SBOMRow[], after: SBOMRow[]): SBOMChange[] {
  const index = (rows: SBOMRow[]) => {
    const m = new Map<string, { name: string; ecosystem: string; versions: Set<string> }>();
    for (const r of rows) {
      const k = `${r.ecosystem}\u0000${r.name}`;
      const e = m.get(k) ?? { name: r.name, ecosystem: r.ecosystem, versions: new Set<string>() };
      e.versions.add(r.version || "—");
      m.set(k, e);
    }
    return m;
  };
  const a = index(before);
  const b = index(after);
  const fmt = (s: Set<string>) => [...s].sort().join(", ");
  const out: SBOMChange[] = [];
  for (const [k, e] of b) {
    const old = a.get(k);
    if (!old) out.push({ kind: "added", name: e.name, ecosystem: e.ecosystem, to: fmt(e.versions) });
    else if (fmt(old.versions) !== fmt(e.versions))
      out.push({ kind: "updated", name: e.name, ecosystem: e.ecosystem, from: fmt(old.versions), to: fmt(e.versions) });
  }
  for (const [k, e] of a) {
    if (!b.has(k)) out.push({ kind: "removed", name: e.name, ecosystem: e.ecosystem, from: fmt(e.versions) });
  }
  const order = { updated: 0, added: 1, removed: 2 };
  return out.sort((x, y) => order[x.kind] - order[y.kind] || x.name.localeCompare(y.name));
}
