"use client";

// QuillAuthContext gives client components one provider-neutral auth API:
//   getToken(): the backend bearer (Zitadel access token)
//   signOut():  end the session and return to /sign-in
// The zitadel-bridge fills it using next-auth's hooks, so consumers never import
// next-auth/react directly.
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
