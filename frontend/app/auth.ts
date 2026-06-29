// NextAuth (Auth.js v5) configuration for the Zitadel OIDC provider. Only used
// when NEXT_PUBLIC_AUTH_PROVIDER=zitadel; under Clerk these exports are never
// invoked. Zitadel is a public PKCE client (no secret), so the access token from
// the auth-code exchange is surfaced on the session for the backend bearer.
import NextAuth from "next-auth";
import Zitadel from "next-auth/providers/zitadel";

const issuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER ?? "";
const clientId = process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID ?? "";

export const { handlers, auth, signIn, signOut } = NextAuth({
  trustHost: true,
  pages: { signIn: "/sign-in" },
  // Only configure the provider when an issuer is set (i.e. under Zitadel). Under
  // Clerk this module is imported but never invoked, so an empty provider list is
  // fine and avoids constructing the OIDC client with placeholder values.
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
    // Expose the access token on the session for client + server reads.
    async session({ session, token }) {
      (session as { accessToken?: string }).accessToken =
        token.accessToken as string | undefined;
      return session;
    },
    // Used by the NextAuth middleware: a signed-in user is authorized.
    authorized({ auth: session }) {
      return !!session?.user;
    },
  },
});
