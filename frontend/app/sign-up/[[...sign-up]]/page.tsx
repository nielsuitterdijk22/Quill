import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";

// Zitadel registration is handled by the IdP (and SSO/SCIM in orgs), so sign-up
// opens the hosted registration form directly.
export default function SignUpPage() {
  return <ZitadelSignInButton label="Create account" register />;
}
