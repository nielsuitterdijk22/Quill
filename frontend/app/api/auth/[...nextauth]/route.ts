// NextAuth (Auth.js) route handlers for the Zitadel OIDC flow: sign-in, callback,
// session, sign-out. Inert under Clerk (the routes exist but are never hit).
import { handlers } from "../../../auth";

export const { GET, POST } = handlers;
