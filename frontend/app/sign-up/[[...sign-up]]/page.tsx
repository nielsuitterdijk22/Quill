import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";

// Zitadel registration is handled by the IdP (and SSO/SCIM in orgs), so sign-up
// routes to the same hosted login.
export default function SignUpPage() {
  return <ZitadelSignInButton label="Continue" />;
}
