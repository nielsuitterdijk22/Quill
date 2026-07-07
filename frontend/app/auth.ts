// NextAuth (Auth.js v5) configuration for the Zitadel OIDC provider. Zitadel is
// a public PKCE client (no secret), so the access token from the auth-code
// exchange is surfaced on the session for the backend bearer.
import NextAuth from "next-auth";
import Zitadel from "next-auth/providers/zitadel";

const issuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER ?? "";
const clientId = process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID ?? "";

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
          // Public PKCE client — no secret. Tell the OIDC client not to
          // authenticate at the token endpoint and to use PKCE + state.
          clientSecret: "",
          client: { token_endpoint_auth_method: "none" },
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
