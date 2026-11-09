// NextAuth (Auth.js v5) configuration for the Zitadel OIDC provider. Zitadel is
// a public PKCE client (no secret), so the access token from the auth-code
// exchange is surfaced on the session for the backend bearer.
import NextAuth from "next-auth";
import Zitadel from "next-auth/providers/zitadel";

const issuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER ?? "";
const clientId = process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID ?? "";
// When the Zitadel OIDC app is switched to a confidential client, a secret
// here enables refresh-token rotation: sessions renew themselves instead of
// silently going stale after the access token's ~1h TTL (the "session expired"
// state). Empty keeps today's public PKCE client.
const clientSecret = process.env.ZITADEL_CLIENT_SECRET ?? "";

export const { handlers, auth, signIn, signOut } = NextAuth({
  trustHost: true,
  pages: { signIn: "/sign-in" },
  // Only configure the provider when an issuer is set, avoiding construction of
  // the OIDC client with placeholder values in environments where Zitadel is not
  // configured (e.g. local-auth development).
  providers: issuer
    ? [
        Zitadel({
          issuer,
          clientId,
          // Public PKCE client when no secret is set; confidential (basic)
          // with one, which additionally allows refresh-token use.
          clientSecret,
          client: {
            token_endpoint_auth_method: clientSecret ? "client_secret_basic" : "none",
          },
          checks: ["pkce", "state"],
          authorization: {
            params: {
              scope: `openid profile email offline_access urn:zitadel:iam:org:project:id:${process.env.NEXT_PUBLIC_ZITADEL_PROJECT_ID ?? "zitadel"}:aud`,
              // Sign-out only ends Quill's own session, not Zitadel's IdP
              // session (see zitadel-bridge.tsx) — without this, a live
              // Zitadel session silently re-authenticates on sign-in with no
              // UI at all. select_account forces Zitadel's account chooser
              // every time instead.
              prompt: "select_account",
            },
          },
        }),
      ]
    : [],
  callbacks: {
    // Persist the Zitadel access + id tokens onto the NextAuth JWT so they can be
    // forwarded to the Quill backend as the bearer.
    async jwt({ token, account }) {
      if (account) {
        token.accessToken = account.access_token;
        token.idToken = account.id_token;
        token.expiresAt = account.expires_at;
        token.refreshToken = account.refresh_token;
        return token;
      }
      // Refresh an expired access token when the provider is confidential.
      // Without a secret (public client) there is nothing to refresh — the
      // session goes stale and the app layout sends the user to re-auth
      // (see /sign-in?stale=1).
      const expiresAt = (token.expiresAt as number | undefined) ?? 0;
      if (
        clientSecret &&
        expiresAt &&
        Date.now() / 1000 > expiresAt - 30 &&
        typeof token.refreshToken === "string" &&
        token.refreshToken !== ""
      ) {
        try {
          const res = await fetch(`${issuer}/oauth/v2/token`, {
            method: "POST",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: new URLSearchParams({
              grant_type: "refresh_token",
              refresh_token: token.refreshToken,
              client_id: clientId,
              client_secret: clientSecret,
            }),
          });
          if (res.ok) {
            const t = (await res.json()) as {
              access_token?: string;
              id_token?: string;
              refresh_token?: string;
              expires_in?: number;
            };
            if (t.access_token) {
              token.accessToken = t.access_token;
              if (t.id_token) token.idToken = t.id_token;
              if (t.refresh_token) token.refreshToken = t.refresh_token;
              token.expiresAt = Math.floor(Date.now() / 1000) + (t.expires_in ?? 3600);
            }
          }
        } catch {
          // Network hiccup — leave the token; the layout forces re-auth if
          // verification keeps failing.
        }
      }
      return token;
    },
    // Expose the access + id tokens on the session for client + server reads.
    // idToken is needed as the id_token_hint on Zitadel's RP-initiated logout
    // (see zitadel-bridge.tsx) so signing out of Quill also ends the Zitadel
    // IdP session instead of just the local app session.
    async session({ session, token }) {
      (session as { accessToken?: string; idToken?: string }).accessToken =
        token.accessToken as string | undefined;
      (session as { accessToken?: string; idToken?: string }).idToken =
        token.idToken as string | undefined;
      return session;
    },
    // Used by the NextAuth middleware: a signed-in user is authorized.
    authorized({ auth: session }) {
      return !!session?.user;
    },
  },
});
