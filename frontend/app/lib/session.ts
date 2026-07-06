// Server-only session helpers, provider-neutral. The active IdP's bearer token
// (Clerk session JWT or Zitadel access token) is forwarded to the Quill backend,
// which verifies it against that provider's JWKS. Selected by
// NEXT_PUBLIC_AUTH_PROVIDER (see lib/auth-provider).
import { auth as clerkAuth, currentUser as clerkCurrentUser } from "@clerk/nextjs/server";
import { redirect } from "next/navigation";

import { auth as zitadelAuth } from "../auth";
import { isZitadel } from "./auth-provider";
import { fetchMe, type User } from "./api";

// getToken returns the current IdP bearer token for Quill backend calls, or
// undefined when the user is not signed in.
export async function getToken(): Promise<string | undefined> {
  if (isZitadel) {
    const session = await zitadelAuth();
    const token = (session as { accessToken?: string } | null)?.accessToken;
    return token ?? undefined;
  }
  const { getToken } = await clerkAuth();
  const token = await getToken();
  return token ?? undefined;
}

// getSession resolves the current Quill user via the backend, or null when
// unauthenticated.
//
// fetchMe failing is treated as "not signed in", not an error. This is what
// breaks the login loop: the NextAuth session cookie outlives the ~1h Zitadel
// access token it carries, so once the token expires the middleware still sees a
// session and lets the request through, but fetchMe gets a 401. Returning null
// (rather than throwing) lets requireSession send the user cleanly to /sign-in
// to re-authenticate, instead of erroring or bouncing indefinitely. A transient
// backend outage degrades to the same signed-out state rather than a crash.
export async function getSession(): Promise<User | null> {
  const token = await getToken();
  if (!token) return null;
  try {
    return await fetchMe(token);
  } catch {
    return null;
  }
}

// requireSession returns the current Quill user, redirecting to /sign-in when
// the session is absent. The auth middleware already protects authenticated
// routes, so this redirect is a belt-and-suspenders guard.
export async function requireSession(): Promise<User> {
  const user = await getSession();
  if (!user) redirect("/sign-in");
  return user;
}

// getProfileAvatar returns the signed-in user's avatar URL from the IdP, or null
// when none is available. Clerk exposes it on currentUser; Zitadel avatars come
// from the userinfo "picture" claim (not surfaced yet), so null for now.
export async function getProfileAvatar(): Promise<string | null> {
  if (isZitadel) return null;
  const user = await clerkCurrentUser();
  return user?.imageUrl ?? null;
}
