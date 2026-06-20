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
