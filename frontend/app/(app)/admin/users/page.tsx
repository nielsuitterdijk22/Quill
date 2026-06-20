import { notFound } from "next/navigation";

import { listAdminUsers } from "../../../lib/api";
import { getToken, requireSession } from "../../../lib/session";
import { UserRow } from "./UserRow";

export default async function AdminUsersPage() {
  const user = await requireSession();
  if (!user.isAdmin) notFound();

  const token = getToken();
  if (!token) notFound();

  const res = await listAdminUsers(token);
  if (!res.ok) {
    return (
      <>
        <div className="crumbs">
          <span>Admin</span> <span>/</span> <span>Users</span>
        </div>
        <h1>Users</h1>
        <div className="banner">{res.message}</div>
      </>
    );
  }

  const users = res.data.users;

  return (
    <>
      <div className="crumbs">
        <span>Admin</span> <span>/</span> <span>Users</span>
      </div>
      <h1>Users</h1>
      <div className="panel">
        {users.length === 0 ? (
          <div className="empty">No users found.</div>
        ) : (
          users.map((u) => (
            <UserRow key={u.id} user={u} />
          ))
        )}
      </div>
    </>
  );
}
