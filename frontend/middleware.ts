// Route protection middleware. Picks Clerk's or NextAuth's middleware based on
// the build-time NEXT_PUBLIC_AUTH_PROVIDER flag. The same public routes are open
// under both providers.
import { clerkMiddleware, createRouteMatcher } from "@clerk/nextjs/server";
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

import { auth as zitadelAuth } from "./app/auth";
import { isZitadel } from "./app/lib/auth-provider";

const PUBLIC_ROUTES = [
  /^\/sign-in(?:\/.*)?$/,
  /^\/sign-up(?:\/.*)?$/,
  /^\/login(?:\/.*)?$/, // kept for backwards-compat redirects
  /^\/register(?:\/.*)?$/,
  /^\/api\/auth(?:\/.*)?$/, // NextAuth endpoints
];

function isPublic(pathname: string): boolean {
  return PUBLIC_ROUTES.some((re) => re.test(pathname));
}

// Clerk: protect everything except the public routes.
const isClerkPublic = createRouteMatcher([
  "/sign-in(.*)",
  "/sign-up(.*)",
  "/login(.*)",
  "/register(.*)",
  "/api/auth(.*)",
]);

function buildClerk() {
  return clerkMiddleware(async (auth, request) => {
    if (!isClerkPublic(request)) {
      await auth.protect();
    }
  });
}

// NextAuth: redirect unauthenticated users on protected routes to /sign-in.
function buildZitadel() {
  return zitadelAuth((req) => {
    const { pathname } = req.nextUrl;
    if (isPublic(pathname) || req.auth?.user) return NextResponse.next();
    const url = req.nextUrl.clone();
    url.pathname = "/sign-in";
    return NextResponse.redirect(url);
  });
}

// Construct only the active provider's middleware — the ternary evaluates one
// branch, so the unused provider is never built at module load.
const middleware = isZitadel ? buildZitadel() : buildClerk();

export default middleware as unknown as (req: NextRequest) => Response;

export const config = {
  matcher: [
    // Skip Next.js internals and static files.
    "/((?!_next|[^?]*\\.(?:html?|css|js(?!on)|jpe?g|webp|png|gif|svg|ttf|woff2?|ico|csv|docx?|xlsx?|zip|webmanifest)).*)",
    "/(api|trpc)(.*)",
  ],
};
