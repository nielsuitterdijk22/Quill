"use client";

// QuillAuthContext gives client components one provider-neutral auth API:
//   getToken(): the backend bearer (Clerk session JWT or Zitadel access token)
//   signOut():  end the session and return to /sign-in
// A per-provider bridge (clerk-bridge / zitadel-bridge) fills it using that
// provider's hooks, so consumers never import @clerk/nextjs or next-auth/react.
import { createContext, useContext } from "react";

export type QuillAuth = {
  getToken: () => Promise<string | null>;
  signOut: () => void;
};

const QuillAuthContext = createContext<QuillAuth | null>(null);

export const QuillAuthProvider = QuillAuthContext.Provider;

export function useQuillAuth(): QuillAuth {
  const ctx = useContext(QuillAuthContext);
  if (!ctx) {
    throw new Error("useQuillAuth must be used within the app AuthProvider");
  }
  return ctx;
}
