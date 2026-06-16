import Link from "next/link";

import { NewProjectForm } from "./NewProjectForm";

// NewProjectPage renders the create-project form. The static "new" segment is
// reserved by the backend so it never collides with a real project slug.
export default function NewProjectPage() {
  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span> <span>New</span>
      </div>
      <h1>Create a project</h1>
      <NewProjectForm />
    </>
  );
}
