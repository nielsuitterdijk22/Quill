# Invite members

Adding someone to Quill is a two-step process: first they join the org, then they're added to one or more projects.

## Step 1 — Invite to the org

Only org owners can send org invites.

Go to **org Settings → Members → Invite member**. Enter their email address and click **Send invite**.

They'll receive an email with a link. If they don't have a Quill account, they'll be prompted to create one. Once they accept, they show up in the org member list with the **Member** role.

Invites expire after 7 days. To resend or revoke a pending invite, go to **org Settings → Members → Pending invites**.

## Step 2 — Add to a project

Once someone is an org member, a project owner can add them to specific projects.

Go to **project Settings → Members → Add member**. Search by name or username and click **Add**.

They now have read and write access to all repos in that project.

## Roles

**Org roles** (set when the org invite is accepted, changeable in org Settings):
- **Member** — default. Access is controlled entirely by project membership.
- **Owner** — full access to everything in the org including all projects.

**Project roles** (set per project):
- **Member** — read/write access to repos in this project.
- **Owner** — can manage project settings, policies, and membership.

## Remove a member

**From a project** — project Settings → Members → remove icon next to the member.

**From the org** — org Settings → Members → remove. This also removes them from all projects in the org.

Removing someone from the org immediately revokes their access. Any PRs they opened stay open; commits stay in history.

## What members can see

An org member can only see the projects and repos they've been explicitly added to (unless they're an org owner). They won't see other projects exist, only their own.
