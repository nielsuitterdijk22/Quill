"use client";

// ZitadelAuthBridge adapts next-auth/react to the provider-neutral QuillAuth
// context. Mounted inside <SessionProvider>. The access token is surfaced on the
// session by the jwt/session callbacks in app/auth.ts.
import { signOut as nextSignOut, useSession } from "next-auth/react";
import { useMemo } from "react";

import { QuillAuthProvider, type QuillAuth } from "./context";

const issuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER ?? "";

// Plain next-auth signOut only clears Quill's own session cookie — Zitadel's
// IdP session cookie survives, so the next sign-in silently re-authenticates
// against it with no login/account UI at all. RP-initiated logout
// (https://openid.net/specs/openid-connect-rpinitiated-1_0.html) ends the
// IdP session too. The endpoint is looked up via discovery rather than
// hardcoded since it has moved between Zitadel API versions. Returns true if
// it navigated the browser away (so the caller shouldn't also redirect).
async function endZitadelSession(
  idToken: string | undefined,
): Promise<boolean> {
  if (!issuer) return false;
  try {
    const res = await fetch(`${issuer}/.well-known/openid-configuration`);
    const config = (await res.json()) as { end_session_endpoint?: string };
    if (!config.end_session_endpoint) return false;
    const url = new URL(config.end_session_endpoint);
    if (idToken) url.searchParams.set("id_token_hint", idToken);
    url.searchParams.set(
      "post_logout_redirect_uri",
      `${window.location.origin}/sign-in`,
    );
    window.location.href = url.toString();
    return true;
  } catch {
    // Discovery unreachable — fall back to just the local sign-out.
    return false;
  }
}

export function ZitadelAuthBridge({ children }: { children: React.ReactNode }) {
  const { data: session } = useSession();

  const value = useMemo<QuillAuth>(
    () => ({
      getToken: async () =>
        (session as { accessToken?: string } | null)?.accessToken ?? null,
      signOut: () => {
        void (async () => {
          const idToken = (session as { idToken?: string } | null)?.idToken;
          await nextSignOut({ redirect: false });
          const navigated = await endZitadelSession(idToken);
          console.log("ZitadelAuthBridge.signOut: navigated away?", navigated);
          if (!navigated) window.location.href = "/sign-in";
        })();
      },
    }),
    [session],
  );

  return <QuillAuthProvider value={value}>{children}</QuillAuthProvider>;
}
