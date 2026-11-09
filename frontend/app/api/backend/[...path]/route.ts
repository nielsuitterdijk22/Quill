// Server-side proxy for browser calls to the Go backend.
//
// Client components fetch /api/backend/<path>; this handler forwards the
// request to QUILL_API_BASE_URL/api/v1/<path>. The rewrite in
// next.config.mjs pointed here once too, but a rewrite can't authenticate:
// in the Zitadel deployment the browser holds an Auth.js session cookie,
// not the backend's bearer, so bare /api/backend/* calls used to reach the
// Go API unauthenticated and 401 ("authentication required") — which broke
// the Pro checkout button and silently emptied the SSH key / git token
// panels. This handler injects the session's access token as the bearer.
//
// The caller-visible rules:
//   - an explicit Authorization header wins (external services, e.g. Atlas,
//     forward their own caller token) and is passed through untouched;
//   - otherwise the signed-in session's access token is attached, and
//     state-changing methods must be same-origin (belt-and-suspenders next
//     to the SameSite=Lax session cookie — a cross-site form can't forge
//     our JSON bodies anyway);
//   - anonymous GETs pass through without a token (public repo reads).
//
// This route takes precedence over the next.config.mjs rewrite (filesystem
// routes are matched before afterFiles rewrites).

import { NextRequest, NextResponse } from "next/server";

import { auth } from "../../../auth";

const API = process.env.QUILL_API_BASE_URL || "http://localhost:8080";

const BODYLESS = new Set(["GET", "HEAD"]);

function sameOrigin(req: NextRequest): boolean {
  const origin = req.headers.get("origin") ?? req.headers.get("referer");
  if (!origin) return true; // non-browser callers (curl, services)
  try {
    return new URL(origin).host === req.headers.get("host");
  } catch {
    return false;
  }
}

async function proxy(
  req: NextRequest,
  ctx: { params: Promise<{ path: string[] }> },
) {
  const { path } = await ctx.params;
  const url = new URL(req.url);
  const target = `${API}/api/v1/${path.join("/")}${url.search}`;

  const headers = new Headers();
  const contentType = req.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);

  let authorization = req.headers.get("authorization");
  if (!authorization) {
    const session = await auth();
    const token = (session as { accessToken?: string } | null)?.accessToken;
    if (token) {
      if (!BODYLESS.has(req.method) && !sameOrigin(req)) {
        return NextResponse.json(
          { error: "forbidden", message: "cross-origin request rejected" },
          { status: 403 },
        );
      }
      authorization = `Bearer ${token}`;
    }
  }
  if (authorization) headers.set("authorization", authorization);

  const init: RequestInit & { duplex?: "half" } = { method: req.method, headers };
  if (!BODYLESS.has(req.method)) {
    init.body = await req.arrayBuffer();
    if (init.body.byteLength === 0) delete init.body;
  }

  const res = await fetch(target, init).catch(() => null);
  if (!res) {
    return NextResponse.json(
      { error: "unreachable", message: "Can't reach the Quill backend." },
      { status: 502 },
    );
  }
  const out = new NextResponse(res.body, { status: res.status });
  for (const h of ["content-type", "content-disposition"]) {
    const v = res.headers.get(h);
    if (v) out.headers.set(h, v);
  }
  return out;
}

export {
  proxy as GET,
  proxy as HEAD,
  proxy as POST,
  proxy as PUT,
  proxy as PATCH,
  proxy as DELETE,
  proxy as OPTIONS,
};
