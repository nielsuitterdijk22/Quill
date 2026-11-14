import { authGet, sendData } from "./client";
import type { DataResult } from "./types";

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

// Emails the user's commits are attributed to on the contribution graph: the
// account email always counts; `emails` are extra addresses (work laptop,
// GitHub noreply, …).
export type CommitEmails = { primary: string; emails: string[] };

export async function getCommitEmails(token: string): Promise<CommitEmails | null> {
  const res = await authGet<CommitEmails>(token, "/api/v1/me/commit-emails");
  return res.ok ? res.data : null;
}

export function setCommitEmails(token: string, emails: string[]): Promise<DataResult<CommitEmails>> {
  return sendData<CommitEmails>(token, "PUT", "/api/v1/me/commit-emails", { emails });
}
