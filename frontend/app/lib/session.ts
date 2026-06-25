// Server-only session helpers backed by Clerk authentication.
// The Clerk session JWT is forwarded to the Quill backend as a Bearer token;
// the backend verifies it against Clerk's JWKS endpoint.
import { auth } from "@clerk/nextjs/server";
import { redirect } from "next/navigation";

import { fetchMe, type User } from "./api";

// getToken returns the current Clerk session JWT for use as a Bearer token in
// Quill backend API calls, or undefined when the user is not signed in.
export async function getToken(): Promise<string | undefined> {
  const { getToken } = await auth();
  const token = await getToken();
  return token ?? undefined;
}

// getSession resolves the current Quill user via the backend, or null when
// unauthenticated.
export async function getSession(): Promise<User | null> {
  const token = await getToken();
  if (!token) return null;
  return fetchMe(token);
}

// requireSession returns the current Quill user, redirecting to /sign-in when
// the session is absent. The Clerk middleware already protects authenticated
// routes, so this redirect is a belt-and-suspenders guard.
export async function requireSession(): Promise<User> {
  const user = await getSession();
  if (!user) redirect("/sign-in");
  return user;
}
