// Server-only session helpers. The access token lives in an httpOnly cookie set
// by the auth server actions; these helpers read it, resolve the current user via
// the backend, and gate the authenticated shell.
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { fetchMe, type User } from "./api";

export const SESSION_COOKIE = "quill_token";

// Keep the cookie lifetime aligned with the backend's default token TTL (24h).
// The JWT itself remains the source of truth for expiry.
const MAX_AGE_SECONDS = 60 * 60 * 24;

export function getToken(): string | undefined {
  return cookies().get(SESSION_COOKIE)?.value;
}

// getSession resolves the current user, or null when unauthenticated.
export async function getSession(): Promise<User | null> {
  const token = getToken();
  if (!token) return null;
  return fetchMe(token);
}

// requireSession returns the current user or redirects to /login. Use it in the
// authenticated shell so protected pages never render without a user.
export async function requireSession(): Promise<User> {
  const user = await getSession();
  if (!user) redirect("/login");
  return user;
}

export function setSessionCookie(token: string): void {
  cookies().set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: MAX_AGE_SECONDS,
  });
}

export function clearSessionCookie(): void {
  cookies().delete(SESSION_COOKIE);
}
