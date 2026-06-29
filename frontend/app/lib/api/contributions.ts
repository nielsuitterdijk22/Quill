import { authGet } from "./client";

// One day's contribution bucket from the backend (proxied from Forgejo's
// heatmap). `timestamp` is Unix seconds; the backend may return several entries
// for the same day, so callers should sum by date.
export type Contribution = { timestamp: number; contributions: number };

// getMyContributions returns the signed-in user's commit-activity series for
// the contribution graph. Returns [] on any failure so the profile still
// renders (an empty graph) rather than erroring.
export async function getMyContributions(token: string): Promise<Contribution[]> {
  const res = await authGet<{ contributions?: Contribution[] }>(token, "/api/v1/me/contributions");
  return res.ok ? (res.data.contributions ?? []) : [];
}
