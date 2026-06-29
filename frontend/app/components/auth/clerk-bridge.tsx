"use client";

// ClerkAuthBridge adapts Clerk's hooks to the provider-neutral QuillAuth context.
// Mounted only when NEXT_PUBLIC_AUTH_PROVIDER=clerk, inside <ClerkProvider>.
import { useMemo } from "react";
import { useAuth, useClerk } from "@clerk/nextjs";

import { QuillAuthProvider, type QuillAuth } from "./context";

export function ClerkAuthBridge({ children }: { children: React.ReactNode }) {
  const { getToken } = useAuth();
  const { signOut } = useClerk();

  const value = useMemo<QuillAuth>(
    () => ({
      getToken: () => getToken(),
      signOut: () => {
        void signOut({ redirectUrl: "/sign-in" });
      },
    }),
    [getToken, signOut],
  );

  return <QuillAuthProvider value={value}>{children}</QuillAuthProvider>;
}
