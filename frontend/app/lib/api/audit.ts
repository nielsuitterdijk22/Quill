import { authGet } from "./client";
import type { Result } from "./types";

export type AuditLogEntry = {
  id: number;
  action: string;
  targetType: string;
  targetId: string;
  metadata: Record<string, unknown>;
  actorUsername: string;
  ipAddress: string;
  createdAt: string;
};

export type AuditLogResult = {
  entries: AuditLogEntry[];
  total: number;
  limit: number;
  offset: number;
};

export function listAuditLog(
  token: string,
  params?: { action?: string; since?: string; until?: string; limit?: number; offset?: number },
): Promise<Result<AuditLogResult>> {
  const q = new URLSearchParams();
  if (params?.action) q.set("action", params.action);
  if (params?.since) q.set("since", params.since);
  if (params?.until) q.set("until", params.until);
  if (params?.limit) q.set("limit", String(params.limit));
  if (params?.offset) q.set("offset", String(params.offset));
  const qs = q.toString();
  return authGet<AuditLogResult>(token, `/api/v1/admin/audit-log${qs ? `?${qs}` : ""}`);
}

export function auditLogExportUrl(params?: {
  action?: string;
  since?: string;
  until?: string;
}): string {
  const q = new URLSearchParams();
  if (params?.action) q.set("action", params.action);
  if (params?.since) q.set("since", params.since);
  if (params?.until) q.set("until", params.until);
  const qs = q.toString();
  return `/api/backend/admin/audit-log/export${qs ? `?${qs}` : ""}`;
}
