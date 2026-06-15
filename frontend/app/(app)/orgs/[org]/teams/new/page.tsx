import Link from "next/link";

import { NewTeamForm } from "./NewTeamForm";

// NewTeamPage renders the create-team form for an organization. The static "new"
// segment is reserved by the backend so it never collides with a real team slug.
export default function NewTeamPage({ params }: { params: { org: string } }) {
  return (
    <>
      <div className="crumbs">
        <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
        <Link href={`/orgs/${params.org}`}>{params.org}</Link> <span>/</span>{" "}
        <Link href={`/orgs/${params.org}/teams`}>Teams</Link> <span>/</span>{" "}
        <span>New</span>
      </div>
      <h1>Create a team</h1>
      <NewTeamForm org={params.org} />
    </>
  );
}
