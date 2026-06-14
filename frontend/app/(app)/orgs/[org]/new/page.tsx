import Link from "next/link";

import { NewRepoForm } from "./NewRepoForm";

// NewRepoPage renders the create-repository form for an org. The static "new"
// segment is reserved by the backend so it never collides with a repo slug.
export default function NewRepoPage({ params }: { params: { org: string } }) {
  return (
    <>
      <div className="crumbs">
        <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
        <Link href={`/orgs/${params.org}`}>{params.org}</Link> <span>/</span>{" "}
        <span>New</span>
      </div>
      <h1>Create a repository</h1>
      <NewRepoForm org={params.org} />
    </>
  );
}
