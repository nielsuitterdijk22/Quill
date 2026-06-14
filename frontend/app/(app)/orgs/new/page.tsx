import Link from "next/link";

import { NewOrgForm } from "./NewOrgForm";

// NewOrgPage renders the create-organization form. The static "new" segment is
// reserved by the backend so it never collides with a real org slug.
export default function NewOrgPage() {
  return (
    <>
      <div className="crumbs">
        <Link href="/orgs">Organizations</Link> <span>/</span> <span>New</span>
      </div>
      <h1>Create an organization</h1>
      <NewOrgForm />
    </>
  );
}
