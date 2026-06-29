// Auth provider selector. NEXT_PUBLIC_AUTH_PROVIDER is inlined at build time by
// Next.js, so this constant is a true compile-time value safe to branch on in
// both server and client code (including for picking which provider's hooks to
// call — the value never changes between renders).
export type AuthProviderName = "clerk" | "zitadel";

export const AUTH_PROVIDER: AuthProviderName =
  process.env.NEXT_PUBLIC_AUTH_PROVIDER === "zitadel" ? "zitadel" : "clerk";

export const isZitadel = AUTH_PROVIDER === "zitadel";
export const isClerk = AUTH_PROVIDER === "clerk";
