import { authGet, postNoContent, sendNoContent } from "./client";
import type { User, Result } from "./types";

export function listAdminUsers(token: string): Promise<Result<{ users: User[] }>> {
  return authGet<{ users: User[] }>(token, "/api/v1/admin/users");
}

export function adminSetUserActive(
  token: string,
  username: string,
  active: boolean,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return sendNoContent(token, "PATCH", `/api/v1/admin/users/${encodeURIComponent(username)}/active`, { active });
}

export function adminResetPassword(
  token: string,
  username: string,
  newPassword: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return postNoContent(token, `/api/v1/admin/users/${encodeURIComponent(username)}/reset-password`, { newPassword });
}

export type AnalyticsData = {
  windowDays: number;
  generatedAt: string;
  totalSignups: number;
  windowSignups: number;
  activeUsers: number;
  signupsByDay: { day: string; count: number }[];
  funnel: { key: string; label: string; users: number }[];
  signups: {
    username: string;
    createdAt: string;
    plan: string;
    isAdmin: boolean;
    lastSeen?: string;
    reached: Record<string, string>;
  }[];
  tenants: {
    slug: string;
    name: string;
    plan: string;
    personal: boolean;
    members: number;
    repos: number;
    minutes: number;
    minuteLimit: number;
    storageBytes: number;
    storageLimit: number;
    nearLimit: boolean;
    createdAt: string;
  }[];
  features: {
    feature: string;
    action: string;
    events: number;
    users: number;
    tenants: number;
    previous: number;
  }[];
  runs: { total: number; succeeded: number; failed: number; minutes: number };
};

// getAnalytics loads operator analytics (admins only) for the last `days` days.
export function getAnalytics(token: string, days: number): Promise<Result<AnalyticsData>> {
  return authGet<AnalyticsData>(token, `/api/v1/admin/analytics?days=${days}`);
}

// Beta invites: temporary, capped links that put a hand-picked user on the
// free beta plan (platform admins only).
export type BetaInvite = {
  id: string;
  note: string;
  maxUses: number;
  uses: number;
  expiresAt: string;
  revokedAt?: string;
  createdAt: string;
  active: boolean;
};

export function listBetaInvites(token: string): Promise<Result<{ invites: BetaInvite[] }>> {
  return authGet<{ invites: BetaInvite[] }>(token, "/api/v1/admin/beta-invites");
}
