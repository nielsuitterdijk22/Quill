import Link from "next/link";
import { notFound } from "next/navigation";

import { getAnalytics, type AnalyticsData } from "../../../lib/api";
import { getToken, requireSession } from "../../../lib/session";
import { PlanSelect } from "./PlanSelect";

export const metadata = { title: "Analytics" };

const WINDOWS = [7, 30, 90];

function fmtDay(iso: string): string {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

function ago(iso?: string): string {
  if (!iso) return "never";
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 60) return `${Math.max(mins, 1)}m ago`;
  const h = Math.round(mins / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

function bytes(n: number): string {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(2)} GiB`;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MiB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KiB`;
  return `${n} B`;
}

// Meter shows usage against a free-plan gate (paid plans: no limit).
function Meter({ used, limit, free, label }: { used: number; limit: number; free: boolean; label: string }) {
  const pct = free && limit > 0 ? Math.min(100, (used / limit) * 100) : 0;
  return (
    <div className="an-meter" title={free ? `${Math.round(pct)}% of the free plan` : "no limit on this plan"}>
      <span>{label}</span>
      {free && (
        <span className="an-meter-track">
          <span className={`an-meter-fill${pct >= 80 ? " warn" : ""}`} style={{ width: `${pct}%` }} />
        </span>
      )}
    </div>
  );
}

function SignupBars({ days }: { days: AnalyticsData["signupsByDay"] }) {
  const max = Math.max(1, ...days.map((d) => d.count));
  return (
    <div className="an-bars" role="img" aria-label="Signups per day">
      {days.map((d) => (
        <div
          key={d.day}
          className="an-bar"
          title={`${fmtDay(d.day)}: ${d.count} signup${d.count === 1 ? "" : "s"}`}
        >
          <span style={{ height: `${(d.count / max) * 100}%` }} className={d.count ? "" : "zero"} />
        </div>
      ))}
    </div>
  );
}

export default async function AnalyticsPage({ searchParams }: { searchParams: { days?: string } }) {
  const user = await requireSession();
  if (!user.isAdmin) notFound();
  const token = await getToken();
  if (!token) notFound();

  const days = WINDOWS.includes(Number(searchParams.days)) ? Number(searchParams.days) : 30;
  const res = await getAnalytics(token, days);
  if (!res.ok) {
    return (
      <>
        <div className="crumbs">
          <span>Admin</span> <span>/</span> <span>Analytics</span>
        </div>
        <div className="form-error">Could not load analytics: {res.message}</div>
      </>
    );
  }
  const a = res.data;
  const total = a.totalSignups;
  const byFeature = new Map<string, AnalyticsData["features"]>();
  for (const f of a.features) byFeature.set(f.feature, [...(byFeature.get(f.feature) ?? []), f]);
  const features = Array.from(byFeature.entries())
    .map(([feature, rows]) => ({ feature, rows, events: rows.reduce((n, r) => n + r.events, 0) }))
    .sort((x, y) => y.events - x.events);

  return (
    <>
      <div className="crumbs">
        <span>Admin</span> <span>/</span> <span>Analytics</span>
      </div>
      <div className="page-head an-head">
        <div>
          <h1>Analytics</h1>
          <p className="subtle">
            First-party numbers from Quill&apos;s own data. Test accounts (e2e) are left out.
          </p>
        </div>
        <nav className="state-tabs" aria-label="Window">
          {WINDOWS.map((w) => (
            <Link key={w} href={`/admin/analytics?days=${w}`} className={w === days ? "active" : ""}>
              {w} days
            </Link>
          ))}
        </nav>
      </div>

      <div className="cards">
        <div className="card">
          <div className="k">Accounts</div>
          <div className="v">{total}</div>
        </div>
        <div className="card">
          <div className="k">New in {days} days</div>
          <div className="v">{a.windowSignups}</div>
        </div>
        <div className="card">
          <div className="k">Active in {days} days</div>
          <div className="v">{a.activeUsers}</div>
        </div>
        <div className="card">
          <div className="k">Pipeline runs</div>
          <div className="v">
            {a.runs.total} <small>· {Math.round(a.runs.minutes)} min</small>
          </div>
        </div>
      </div>

      <div className="panel">
        <h2>
          Signups per day <span className="subtle">last {days} days</span>
        </h2>
        <div className="an-pad">
          <SignupBars days={a.signupsByDay} />
          <div className="an-axis">
            <span>{a.signupsByDay[0] ? fmtDay(a.signupsByDay[0].day) : ""}</span>
            <span>today</span>
          </div>
        </div>
      </div>

      <div className="panel">
        <h2>
          Activation <span className="subtle">of {total} accounts, all time</span>
        </h2>
        <div className="an-pad an-funnel">
          <div className="an-step">
            <span className="an-step-label">Signed up</span>
            <span className="an-step-track">
              <span style={{ width: total ? "100%" : "0%" }} />
            </span>
            <span className="an-step-n">{total}</span>
          </div>
          {a.funnel.map((f) => (
            <div className="an-step" key={f.key}>
              <span className="an-step-label">{f.label}</span>
              <span className="an-step-track">
                <span style={{ width: total ? `${(f.users / total) * 100}%` : "0%" }} />
              </span>
              <span className="an-step-n">{f.users}</span>
            </div>
          ))}
        </div>
      </div>

      <div className="panel">
        <h2>Accounts</h2>
        <table className="an-table">
          <thead>
            <tr>
              <th>Account</th>
              <th>Signed up</th>
              <th>Plan</th>
              <th>Last active</th>
              {a.funnel.map((f) => (
                <th key={f.key} className="an-c" title={f.label}>
                  {f.key}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {a.signups.map((s) => (
              <tr key={s.username}>
                <td>
                  <b>{s.username}</b> {s.isAdmin && <span className="badge accent">admin</span>}
                </td>
                <td>{fmtDay(s.createdAt)}</td>
                <td>{s.plan || "—"}</td>
                <td>{ago(s.lastSeen)}</td>
                {a.funnel.map((f) => (
                  <td key={f.key} className="an-c" title={s.reached[f.key] ? `${f.label}: ${fmtDay(s.reached[f.key])}` : `${f.label}: not yet`}>
                    {s.reached[f.key] ? <span className="an-yes">✓</span> : <span className="subtle">·</span>}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="panel">
        <h2>
          Plan usage <span className="subtle">against the free-plan limits</span>
        </h2>
        <table className="an-table">
          <thead>
            <tr>
              <th>Organization</th>
              <th>Plan</th>
              <th className="an-r">Members</th>
              <th>Repositories</th>
              <th>Pipeline minutes (month)</th>
              <th>Storage</th>
            </tr>
          </thead>
          <tbody>
            {a.tenants.map((t) => {
              const free = t.plan === "free";
              return (
                <tr key={t.slug}>
                  <td>
                    <b>{t.name || t.slug}</b> {t.personal && <span className="subtle">personal</span>}{" "}
                    {t.nearLimit && <span className="badge gold">near limit</span>}
                  </td>
                  <td>
                    <PlanSelect slug={t.slug} plan={t.plan} personal={t.personal} />
                  </td>
                  <td className="an-r">{t.personal ? "—" : t.members}</td>
                  <td>
                    <Meter used={t.repos} limit={t.repoLimit} free={free} label={free ? `${t.repos} / ${t.repoLimit}` : `${t.repos}`} />
                  </td>
                  <td>
                    <Meter used={t.minutes} limit={t.minuteLimit} free={free} label={free ? `${Math.round(t.minutes)} / ${t.minuteLimit}` : `${Math.round(t.minutes)}`} />
                  </td>
                  <td>
                    <Meter used={t.storageBytes} limit={t.storageLimit} free={free} label={bytes(t.storageBytes)} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <div className="panel">
        <h2>
          Feature usage <span className="subtle">last {days} days · change vs the {days} days before</span>
        </h2>
        {features.length === 0 ? (
          <div className="an-pad subtle">No activity in this window.</div>
        ) : (
          <table className="an-table">
            <thead>
              <tr>
                <th>Feature / action</th>
                <th className="an-r">Events</th>
                <th className="an-r">Users</th>
                <th className="an-r">Orgs</th>
                <th className="an-r">Change</th>
              </tr>
            </thead>
            <tbody>
              {features.map((g) => [
                <tr key={g.feature} className="an-group">
                  <td colSpan={5}>
                    {g.feature} <span className="subtle">· {g.events} events</span>
                  </td>
                </tr>,
                ...g.rows.map((r) => {
                  const delta = r.events - r.previous;
                  return (
                    <tr key={r.action}>
                      <td className="mono an-indent">{r.action}</td>
                      <td className="an-r">{r.events}</td>
                      <td className="an-r">{r.users}</td>
                      <td className="an-r">{r.tenants}</td>
                      <td className={`an-r ${delta > 0 ? "an-up" : delta < 0 ? "an-down" : "subtle"}`}>
                        {delta > 0 ? `+${delta}` : delta < 0 ? `${delta}` : "0"}
                      </td>
                    </tr>
                  );
                }),
              ])}
            </tbody>
          </table>
        )}
      </div>
      <p className="subtle an-foot">
        Generated {new Date(a.generatedAt).toLocaleString("en-GB")}. Reads (browsing code, viewing runs) aren&apos;t
        counted, only actions in the audit log.
      </p>
    </>
  );
}
