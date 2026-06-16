import Link from "next/link";

import { NewRepoForm } from "./NewRepoForm";

// NewRepoPage renders the create-repository form for an project. The static "new"
// segment is reserved by the backend so it never collides with a repo slug.
export default function NewRepoPage({ params }: { params: { project: string } }) {
  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <Link href={`/projects/${params.project}`}>{params.project}</Link> <span>/</span>{" "}
        <span>New</span>
      </div>
      <h1>Create a repository</h1>
      <NewRepoForm project={params.project} />
    </>
  );
}
