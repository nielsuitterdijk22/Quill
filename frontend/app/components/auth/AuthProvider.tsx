"use client";

// AuthProvider mounts exactly one identity provider's React tree based on the
// build-time NEXT_PUBLIC_AUTH_PROVIDER flag, then a bridge that exposes the
// provider-neutral QuillAuth context to the rest of the app. Both providers are
// imported but only the selected branch renders.
import { ClerkProvider } from "@clerk/nextjs";
import { dark } from "@clerk/themes";
import { SessionProvider } from "next-auth/react";

import { isZitadel } from "../../lib/auth-provider";
import { ClerkAuthBridge } from "./clerk-bridge";
import { ZitadelAuthBridge } from "./zitadel-bridge";

export function AuthProvider({ children }: { children: React.ReactNode }) {
  if (isZitadel) {
    return (
      <SessionProvider>
        <ZitadelAuthBridge>{children}</ZitadelAuthBridge>
      </SessionProvider>
    );
  }
  return (
    <ClerkProvider appearance={{ baseTheme: dark }} afterSignOutUrl="/sign-in">
      <ClerkAuthBridge>{children}</ClerkAuthBridge>
    </ClerkProvider>
  );
}
