// Route protection middleware (NextAuth). Redirects unauthenticated users on
// protected routes to /sign-in; the listed public routes stay open.
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

import { auth as zitadelAuth } from "./app/auth";

const PUBLIC_ROUTES = [
  /^\/sign-in(?:\/.*)?$/,
  /^\/sign-up(?:\/.*)?$/,
  /^\/login(?:\/.*)?$/, // kept for backwards-compat redirects
  /^\/register(?:\/.*)?$/,
  /^\/api\/auth(?:\/.*)?$/, // NextAuth endpoints
  // /api/backend/* proxies to the Go API (next.config.mjs rewrite → /api/v1/*)
  // and carries its own Authorization: Bearer token — from the browser session
  // via app/lib/api.ts, or from external service callers (e.g. Atlas) forwarding
  // a caller's own token directly. Neither has a Quill session cookie, so this
  // middleware must not gate it; the Go backend's requireAuth verifies the
  // bearer itself (mirrors how Atlas's own middleware.ts treats its /api/*
  // backend-proxied routes as public for the same reason).
  /^\/api\/backend(?:\/.*)?$/,
];

function isPublic(pathname: string): boolean {
  return PUBLIC_ROUTES.some((re) => re.test(pathname));
}

// NextAuth: redirect unauthenticated users on protected routes to /sign-in.
const middleware = zitadelAuth((req) => {
  const { pathname } = req.nextUrl;
  if (isPublic(pathname) || req.auth?.user) return NextResponse.next();
  const url = req.nextUrl.clone();
  url.pathname = "/sign-in";
  return NextResponse.redirect(url);
});

export default middleware as unknown as (req: NextRequest) => Response;

export const config = {
  matcher: [
    // Skip Next.js internals and static files.
    "/((?!_next|[^?]*\\.(?:html?|css|js(?!on)|jpe?g|webp|png|gif|svg|ttf|woff2?|ico|csv|docx?|xlsx?|zip|webmanifest)).*)",
    "/(api|trpc)(.*)",
  ],
};
