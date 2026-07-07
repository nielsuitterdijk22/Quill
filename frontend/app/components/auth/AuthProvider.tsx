"use client";

// AuthProvider mounts the Zitadel (NextAuth) session tree and a bridge that
// exposes the provider-neutral QuillAuth context to the rest of the app.
import { SessionProvider } from "next-auth/react";

import { ZitadelAuthBridge } from "./zitadel-bridge";

export function AuthProvider({ children }: { children: React.ReactNode }) {
  return (
    <SessionProvider>
      <ZitadelAuthBridge>{children}</ZitadelAuthBridge>
    </SessionProvider>
  );
}
