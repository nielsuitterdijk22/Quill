"use client";

// ZitadelAuthBridge adapts next-auth/react to the provider-neutral QuillAuth
// context. Mounted only when NEXT_PUBLIC_AUTH_PROVIDER=zitadel, inside
// <SessionProvider>. The access token is surfaced on the session by the jwt/
// session callbacks in app/auth.ts.
import { useMemo } from "react";
import { signOut as nextSignOut, useSession } from "next-auth/react";

import { QuillAuthProvider, type QuillAuth } from "./context";

export function ZitadelAuthBridge({ children }: { children: React.ReactNode }) {
  const { data: session } = useSession();

  const value = useMemo<QuillAuth>(
    () => ({
      getToken: async () =>
        (session as { accessToken?: string } | null)?.accessToken ?? null,
      signOut: () => {
        void nextSignOut({ callbackUrl: "/sign-in" });
      },
    }),
    [session],
  );

  return <QuillAuthProvider value={value}>{children}</QuillAuthProvider>;
}
