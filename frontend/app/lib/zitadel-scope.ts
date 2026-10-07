// Shared Zitadel OIDC scope construction, imported by the Auth.js provider config
// (app/auth.ts). NEXT_PUBLIC_* is readable on client and server.

const projectId = process.env.NEXT_PUBLIC_ZITADEL_PROJECT_ID ?? "zitadel";

// BASE_SCOPE is the scope for every login: OIDC basics, a refresh token
// (offline_access), and the project audience scope the Quill backend requires on
// the access token it verifies. There is no per-org scoping — the org a user
// belongs to is resolved from the token's resource-owner claim server-side, and
// SSO customers are routed to their provider by Zitadel's own domain discovery.
export const BASE_SCOPE = `openid profile email offline_access urn:zitadel:iam:org:project:id:${projectId}:aud`;
