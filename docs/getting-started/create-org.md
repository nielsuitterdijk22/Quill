# Create an organization

When you're ready to collaborate with a team, create an organization. It gives your team a shared namespace, centralized access control, and org-wide policies.

## Create the org

Go to your avatar menu → **New organization**.

- **Name** — shown in the UI and in email notifications.
- **Slug** — becomes your namespace (`quill.so/your-slug`). Lowercase, no spaces. This can't be changed after creation, so choose carefully.

Click **Create organization**. You're now the owner.

## Invite your team

Go to the org's **Settings → Members → Invite member**.

Enter the email address of the person you want to add. They'll get an email with a link to join the org. If they don't have a Quill account yet, they'll be prompted to create one first.

Invites expire after 7 days. You can resend or revoke them from the same page.

## Set up your first project

Inside the org, go to **Projects → New project**. Projects are how you group repos and control who has access to what. See [Projects](../concepts/projects.md) for the full mental model.

## Org vs. personal repos

Repos you created under your personal namespace stay there — they don't automatically move to the org. To work on something as a team, either create a new repo directly in the org, or [fork](../repositories/fork.md) your personal repo into an org project.

## Next steps

- [Invite members](../projects/invite-members.md) — add org members to specific projects
- [Create a project](../projects/create.md) — organize repos into teams
- [Set org policies](../policies/overview.md) — define how code flows across all your repos
