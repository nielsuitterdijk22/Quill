// TEMPORARY diagnostic (remove after the contributions fix is confirmed).
import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { auth } from "../../auth";

const API = process.env.QUILL_API_BASE_URL || "http://localhost:8080";

export async function GET(req: NextRequest) {
  // 1. What cookies does the request carry?
  const cookies: string[] = [];
  req.cookies.forEach((_v, k) => cookies.push(k));

  // 2. What does auth() (used by the dashboard server components) return?
  let sessionKeys: string[] | null = null;
  let viaAuth: string | null = null;
  try {
    const session = (await auth()) as Record<string, unknown> | null;
    sessionKeys = session ? Object.keys(session) : null;
    const t = (session as { accessToken?: string } | null)?.accessToken;
    viaAuth = t ?? null;
  } catch (e) {
    sessionKeys = ["error:" + String(e)];
  }

  // 3. Raw JWT decode with the documented salt variants.
  let viaRaw: string | null = null;
  try {
    const payload = (await getToken({
      req,
      secret: process.env.AUTH_SECRET,
      salt: "__Host-authjs.session-token",
    })) as { accessToken?: string } | null;
    viaRaw = payload?.accessToken ?? null;
  } catch {
    viaRaw = null;
  }

  if (!viaAuth) {
    return NextResponse.json({
      hasToken: false,
      cookies,
      sessionKeys,
      viaRawLen: viaRaw ? viaRaw.length : 0,
    });
  }
  const res = await fetch(`${API}/api/v1/me/contributions`, {
    headers: { Authorization: `Bearer ${viaAuth}` },
    cache: "no-store",
  }).catch(() => null);
  if (!res) {
    return NextResponse.json({ hasToken: true, cookies, sessionKeys, error: "unreachable" });
  }
  const body = await res.text();
  return NextResponse.json({
    hasToken: true,
    cookies,
    sessionKeys,
    viaRawLen: viaRaw ? viaRaw.length : 0,
    tokenLen: viaAuth.length,
    status: res.status,
    body: body.slice(0, 400),
  });
}
