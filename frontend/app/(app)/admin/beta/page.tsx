import { notFound } from "next/navigation";

import { listBetaInvites } from "../../../lib/api";
import { getToken, requireSession } from "../../../lib/session";
import { BetaInviteForm } from "./BetaInviteForm";
import { RevokeButton } from "./RevokeButton";

function status(i: { active: boolean; revokedAt?: string; uses: number; maxUses: number }): string {
  if (i.revokedAt) return "revoked";
  if (i.uses >= i.maxUses) return "used up";
  return i.active ? "active" : "expired";
}

export default async function AdminBetaPage() {
  const user = await requireSession();
  if (!user.isAdmin) notFound();
  const token = await getToken();
  if (!token) notFound();

  const res = await listBetaInvites(token);
  return (
    <>
      <div className="crumbs">
        <span>Admin</span> <span>/</span> <span>Beta invites</span>
      </div>
      <h1>Beta invites</h1>
      <BetaInviteForm />
      {!res.ok ? (
        <div className="banner">{res.message}</div>
      ) : (
        <div className="panel">
          {res.data.invites.length === 0 ? (
            <div className="empty">No beta invites yet.</div>
          ) : (
            <table className="policy-table">
              <thead>
                <tr>
                  <th>For</th>
                  <th>Used</th>
                  <th>Expires</th>
                  <th>Status</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {res.data.invites.map((i) => (
                  <tr key={i.id}>
                    <td>{i.note || "—"}</td>
                    <td className="mono">
                      {i.uses} / {i.maxUses}
                    </td>
                    <td className="mono subtle">{new Date(i.expiresAt).toLocaleDateString()}</td>
                    <td>
                      <span className={`badge ${i.active ? "green" : ""}`}>{status(i)}</span>
                    </td>
                    <td>{i.active && <RevokeButton id={i.id} />}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </>
  );
}
