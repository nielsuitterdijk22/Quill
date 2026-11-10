// TEMPORARY diagnostic (remove after the contributions fix is confirmed):
// reports what the signed-in session token gets from /me/contributions,
// from inside the deployed web container. Exposes only the caller's own data.
import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";

const API = process.env.QUILL_API_BASE_URL || "http://localhost:8080";

export async function GET(req: NextRequest) {
  const payload = (await getToken({
    req,
    secret: process.env.AUTH_SECRET,
    salt: "__Host-authjs.session-token",
  })) as { accessToken?: string } | null;
  const token = payload?.accessToken ?? null;
  if (!token) {
    return NextResponse.json({ hasToken: false, sessionKeys: payload ? Object.keys(payload) : null });
  }
  const res = await fetch(`${API}/api/v1/me/contributions`, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  }).catch(() => null);
  if (!res) return NextResponse.json({ hasToken: true, tokenLen: token.length, error: "unreachable" });
  const body = await res.text();
  return NextResponse.json({
    hasToken: true,
    tokenLen: token.length,
    tokenHead: token.slice(0, 12),
    status: res.status,
    body: body.slice(0, 400),
  });
}
