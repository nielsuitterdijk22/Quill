# User stories

> Owner document. Agents treat it as read-only.

## US-001: Local Email/Password Registration & Login (F1) — exists

As Individual developer, I want to register and sign in using my email and password, so that I can access my free-tier repositories without needing an enterprise SSO setup.

Acceptance criteria:
- [ ] Given I am on the registration page, when I submit a valid email and a password of at least 12 characters, then a new user record is created in the database with a bcrypt-hashed password and a session cookie is set.
- [ ] Given I have an existing account, when I submit my email and correct password to the login endpoint, then a JWT is issued and stored in an HttpOnly, Secure, SameSite=Strict cookie.
- [ ] Given I am on the login page, when I submit an incorrect password, then the system returns a 401 Unauthorized response and no session cookie is set.

## US-002: Enterprise OIDC/SSO Login (Entra) (F1) — exists

As Enterprise developer, I want to sign in using my corporate Microsoft Entra ID credentials, so that I can access company resources securely using my existing corporate identity.

Acceptance criteria:
- [ ] Given my tenant has OIDC configured with an Entra client ID, when I click 'Sign in with SSO', then I am redirected to the Entra authorization endpoint with the correct state parameter.
- [ ] Given I successfully authenticate with Entra, when the IdP redirects back to Quill with a valid code, then Quill exchanges the code for tokens, validates the ID token signature, and creates/logs in the user in the tenant.
- [ ] Given I attempt to log in via SSO, when the ID token contains an email address not associated with the tenant's domain, then the login is rejected with a 'User not found' error.

## US-003: SCIM User Provisioning (F1) — exists

As Tenant Admin, I want Quill to automatically create and deactivate users via SCIM, so that user access aligns with my HR system without manual intervention.

Acceptance criteria:
- [ ] Given a valid SCIM token, when the IdP sends a POST /scim/Users request with a new user's email, then Quill creates the user in the tenant and returns a 201 Created with the new user ID.
- [ ] Given an existing user, when the IdP sends a PATCH /scim/Users/{id} with active: false, then the user's status is set to deactivated and they can no longer log in.
- [ ] Given a request to the SCIM endpoint, when the provided Bearer token is invalid or expired, then Quill returns a 401 Unauthorized and no user records are modified.

## US-004: Session Security Enforcement (F1) — exists

As Security Engineer, I want all session cookies to be strictly secure, so that users are protected from XSS and CSRF attacks.

Acceptance criteria:
- [ ] Given a user logs in, when I inspect the Set-Cookie header in the HTTP response, then the cookie includes flags for HttpOnly, Secure, and SameSite=Strict.
- [ ] Given a user has an active session, when I attempt to access a protected API endpoint without the session cookie, then the system returns a 401 Unauthorized.
- [ ] Given a user logs out, when I inspect the Set-Cookie header, then the session cookie is cleared (Max-Age=0) and subsequent requests with the old cookie are rejected.

## US-005: Create Nested Group Hierarchy (F2) — partial

As Tenant Admin, I want to create groups nested up to 5 levels deep, so that I can mirror my company's organizational structure (e.g., Bank -> Lending -> Fraud).

Acceptance criteria:
- [ ] Given I am a Tenant Admin, when I create a group 'Lending' inside 'Bank', then the new group has a parent_id pointing to 'Bank' and depth=2.
- [ ] Given a group at depth 5, when I attempt to create a child group inside it, then the system returns a 400 Bad Request with error 'Max depth exceeded'.
- [ ] Given I am a non-admin user, when I attempt to create a group via API, then the system returns a 403 Forbidden.

## US-006: Permission Inheritance (F2) — partial

As Developer, I want my access to repositories to inherit from my group memberships, so that I don't need individual permissions for every repo in my department.

Acceptance criteria:
- [ ] Given I am a member of group 'Fraud' with role 'writer', when I list repositories, then I see all repos in 'Fraud' and its sub-groups with effective role 'writer'.
- [ ] Given I have 'reader' role on parent group 'Bank' and 'writer' role on child group 'Lending', when I access a repo in 'Lending', then my effective role is 'writer' (highest privilege wins).
- [ ] Given I have no explicit role on a repo, when I attempt to clone a repo in a group I am not a member of, then the system returns 403 Forbidden.

## US-007: Grant Access via REST API (F2) — partial

As ITSM Tool, I want to grant user access to a project via Quill's API, so that access requests can be automated from my ticketing system.

Acceptance criteria:
- [ ] Given a valid API token with admin scope, when I POST /api/v1/projects/{id}/members with {user: 'sara@bank.com', role: 'reader'}, then the user is added to the project with role 'reader' and a 201 Created is returned.
- [ ] Given a user already has 'writer' access, when I POST to grant 'reader' access, then the existing role is downgraded to 'reader' (or highest privilege is kept, depending on policy) and the change is logged in the audit trail.
- [ ] Given an invalid project ID, when I attempt to grant access, then the system returns a 404 Not Found.

## US-008: Individual Tier Group Limitation (F2) — partial

As System Architect, I want to enforce depth limits for free users, so that free users cannot abuse the nested hierarchy feature.

Acceptance criteria:
- [ ] Given a user on the Individual (Free) tier, when they attempt to create a group inside another group (depth > 1), then the system returns a 403 Forbidden with message 'Nested groups require Enterprise tier'.
- [ ] Given a user on the Individual tier, when they create a top-level group, then the group is created successfully with depth=1.
- [ ] Given a user on the Enterprise tier, when they create a group at depth 5, then the group is created successfully.

## US-009: Create Pull Request with Tempo Link (F3) — exists

As Developer, I want to create a PR and link it to a Tempo ticket, so that my code changes are associated with the work item.

Acceptance criteria:
- [ ] Given I have write access to a repo, when I create a PR with branch 'feature-x' and ticket 'TEMPO-42', then Quill validates the ticket exists in Tempo and creates the PR with the link.
- [ ] Given I attempt to create a PR with ticket 'TEMPO-999' which does not exist, then the system returns a 400 Bad Request with error 'Ticket not found'.
- [ ] Given I create a PR, when I view the PR details, then the Tempo ticket title and current status are displayed inline.

## US-010: Enforce Merge Gates (Reviews + CI) (F3) — exists

As Repo Admin, I want to enforce branch policies requiring approvals and CI passes, so that code is not merged without proper quality checks.

Acceptance criteria:
- [ ] Given a branch policy requires 2 approvals, when a PR has only 1 approval, then the 'Merge' button is disabled and the API returns 409 Conflict if a merge is attempted.
- [ ] Given a branch policy requires CI pass, when the latest pipeline run fails, then the 'Merge' button is disabled.
- [ ] Given a PR has 2 approvals and a passing pipeline, when I click 'Merge', then the PR is merged and the branches are updated.

## US-011: Auto-Transition Tempo Ticket on Merge (F3) — exists

As Project Manager, I want linked tickets to automatically move to 'Done' when code is merged, so that I don't have to manually update ticket status.

Acceptance criteria:
- [ ] Given a PR linked to TEMPO-42 is merged, when the merge event is processed, then Quill calls the Tempo API to transition TEMPO-42 to 'Done'.
- [ ] Given a PR linked to TEMPO-42 is merged, when the Tempo API call fails, then the merge succeeds but an error is logged and the user is notified that the ticket update failed.
- [ ] Given a PR is closed without merging, when the close event is processed, then the Tempo ticket status is NOT changed.

## US-012: PR Diff and Review Interface (F3) — exists

As Reviewer, I want to view the code diff and leave inline comments, so that I can provide specific feedback on code changes.

Acceptance criteria:
- [ ] Given I open a PR, when I view the 'Files Changed' tab, then I see the unified diff with line numbers.
- [ ] Given I am viewing a diff, when I hover over a line and click 'Comment', then I can type a comment and submit it, which is saved to the PR.
- [ ] Given I have reviewed a PR, when I click 'Approve', then my approval is recorded and the approval count increments.

## US-013: Trigger Pipeline on PR Event (F4) — partial

As Developer, I want my CI pipeline to run automatically when I open or update a PR, so that code is tested before review.

Acceptance criteria:
- [ ] Given a repo has a pipeline configured, when a PR is opened, then Quill POSTs the clone URL, commit SHA, and steps to the Forge API.
- [ ] Given a PR is updated with a new commit, when the push event is detected, then a new pipeline run is triggered.
- [ ] Given a pipeline is triggered, when the run starts, then the PR page displays a 'Pipeline Running' status badge.

## US-014: Display Real-Time Pipeline Logs (F4) — partial

As Developer, I want to see the output of my CI steps in the browser, so that I can debug failures without downloading logs.

Acceptance criteria:
- [ ] Given a pipeline is running, when I view the PR page, then the logs are displayed in a scrollable pane, updating as Forge reports progress.
- [ ] Given a pipeline step fails, when I view the logs, then the error output is highlighted in red.
- [ ] Given a pipeline completes, when I view the logs, then the final status (Success/Failure) is displayed at the top.

## US-015: Secret Masking in Logs (F4) — partial

As Security Engineer, I want environment variables marked as secrets to be masked in logs, so that credentials are not leaked in CI output.

Acceptance criteria:
- [ ] Given a pipeline step outputs a value that matches a configured secret, when the logs are displayed, then the secret value is replaced with '***'.
- [ ] Given a pipeline fails and the error message contains a secret, when the error is returned via API, then the secret is masked in the JSON response.

## US-016: Sequential Step Execution (F4) — partial

As DevOps Engineer, I want to define multiple shell steps that run in order, so that I can chain build and test commands.

Acceptance criteria:
- [ ] Given a pipeline config with steps ['go build', 'go test'], when the pipeline runs, then 'go build' executes first, and 'go test' only starts if 'go build' exits with 0.
- [ ] Given the first step fails, when the pipeline continues, then subsequent steps are skipped and the overall status is 'Failed'.

## US-017: Individual Tier Quota Enforcement (F5) — planned

As System Architect, I want to enforce hard limits for free users, so that resources are not abused by free accounts.

Acceptance criteria:
- [ ] Given a free user has 50 repositories, when they attempt to create a 51st repository, then the system returns a 403 Forbidden with message 'Repository limit reached'.
- [ ] Given a free user has used 100 pipeline minutes in the current month, when they trigger a new pipeline, then the system blocks the trigger and returns a 403 Forbidden with message 'Pipeline minute limit reached'.
- [ ] Given a free user's storage usage exceeds 1GB, when they push a new commit, then the push is rejected with a 413 Payload Too Large or similar error.

## US-018: Enterprise Usage-Based Invoicing (F5) — planned

As Tenant Admin, I want to be billed monthly based on actual usage, so that I only pay for what my team uses.

Acceptance criteria:
- [ ] Given the month ends, when the billing cron job runs, then Quill calculates total storage, repo count, and pipeline minutes and creates a Stripe Invoice.
- [ ] Given a Stripe Invoice is created, when the payment is successful, then the invoice status is marked 'Paid' and the user is emailed the receipt.
- [ ] Given a Stripe Invoice is created, when the payment fails, then the invoice status is marked 'Failed' and a retry is scheduled.

## US-019: Usage Dashboard (F5) — planned

As Tenant Admin, I want to view a dashboard of my team's resource consumption, so that I can monitor costs and usage trends.

Acceptance criteria:
- [ ] Given I am a Tenant Admin, when I navigate to the Billing page, then I see current month usage for repos, storage, and pipeline minutes.
- [ ] Given I am a Tenant Admin, when I view the invoices list, then I see a table of past invoices with status and amount.
- [ ] Given I am a non-admin user, when I attempt to access the Billing page, then I am redirected to the home page or receive a 403.

## US-020: Overage Billing for Free Tier (F5) — planned

As Individual Developer, I want to be able to pay for extra usage if I exceed free limits, so that I can continue using the service without hitting hard blocks.

Acceptance criteria:
- [ ] Given a free user exceeds 100 pipeline minutes, when they trigger another pipeline, then they are prompted to add a payment method or auto-charged at €0.05/min.
- [ ] Given a free user has no payment method on file, when they exceed the limit, then the pipeline trigger is blocked and they are directed to add a card.
- [ ] Given a free user has a card on file, when they exceed the limit, then the pipeline runs and the usage is recorded for the next invoice.

## US-021: Browse Repository File Tree (F6) — exists

As Developer, I want to browse the files in a repository, so that I can inspect code without cloning it.

Acceptance criteria:
- [ ] Given I have read access to a repo, when I navigate to the repo page, then I see a list of directories and files in the root of the default branch.
- [ ] Given I click on a directory, when the page loads, then I see the contents of that directory.
- [ ] Given I click on a file, when the page loads, then I see the file contents with syntax highlighting.

## US-022: View Commit History and Branches (F6) — exists

As Developer, I want to see the history of changes and available branches, so that I can track the evolution of the codebase.

Acceptance criteria:
- [ ] Given I am on a repo page, when I click 'Commits', then I see a list of commits for the current branch, newest first.
- [ ] Given I am on a repo page, when I click 'Branches', then I see a list of all branches in the repository.
- [ ] Given I click on a specific commit, when the page loads, then I see the diff for that specific commit.

## US-023: Display Clone URLs (F6) — exists

As Developer, I want to easily copy the HTTPS and SSH clone URLs, so that I can clone the repository to my local machine.

Acceptance criteria:
- [ ] Given I am on a repo page, when I view the 'Clone' section, then I see distinct fields for HTTPS and SSH URLs.
- [ ] Given I click the copy icon next to the HTTPS URL, when I paste into my terminal, then the correct URL is inserted.
- [ ] Given I am an authenticated user, when I view the clone URLs, then the HTTPS URL includes my token or prompts for credentials, and the SSH URL uses my configured key.

## US-024: Repository Metadata Display (F6) — exists

As Developer, I want to see the repository description and visibility, so that I can understand the purpose and access level of the repo.

Acceptance criteria:
- [ ] Given I am on a repo page, when I view the header, then I see the repository name, description, and visibility badge (Public/Private/Internal).
- [ ] Given I am a non-member of a private repo, when I attempt to access the repo URL, then I receive a 404 Not Found (to avoid leaking existence).

## US-025: Record Audit Events (F7) — planned

As Compliance Officer, I want every significant action to be logged, so that I can trace who did what and when.

Acceptance criteria:
- [ ] Given a user merges a PR, when the merge is complete, then an audit record is created with actor_id, action='PR_MERGED', target_id, and timestamp.
- [ ] Given an admin changes a user's role, when the change is saved, then an audit record is created with action='PERMISSION_CHANGED'.
- [ ] Given a billing event occurs, when the invoice is generated, then an audit record is created with action='BILLING_EVENT'.

## US-026: Tenant Isolation Enforcement (F7) — planned

As Security Engineer, I want all database queries to be filtered by tenant_id, so that data from one tenant is never visible to another.

Acceptance criteria:
- [ ] Given a user from Tenant A, when they query the API for repositories, then the SQL query includes a WHERE clause for tenant_id = Tenant A.
- [ ] Given a user from Tenant A, when they attempt to access a resource ID belonging to Tenant B, then the system returns 404 Not Found (not 403, to avoid enumeration).
- [ ] Given a super-admin user, when they query repositories, then the system still enforces tenant_id filtering unless a specific 'cross-tenant' flag is set (which is disabled in v1).

## US-027: GDPR Account Deletion (F7) — planned

As User, I want to delete my account and all my data, so that I can exercise my right to erasure.

Acceptance criteria:
- [ ] Given I am an authenticated user, when I click 'Delete Account' and confirm, then my user record is marked for deletion.
- [ ] Given my account is marked for deletion, when the cleanup job runs, then my profile data is purged from the Quill DB and my user is removed from Forgejo.
- [ ] Given my account is deleted, when I attempt to log in, then the system returns 401 Unauthorized and I cannot recover the account.

## US-028: View Audit Logs (F7) — planned

As Tenant Admin, I want to browse the audit log for my tenant, so that I can investigate security incidents.

Acceptance criteria:
- [ ] Given I am a Tenant Admin, when I navigate to the Audit Log page, then I see a list of recent events filtered to my tenant.
- [ ] Given I am a Tenant Admin, when I filter the log by 'Permission Changes', then only permission-related events are displayed.
- [ ] Given I am a non-admin user, when I attempt to access the Audit Log page, then I receive a 403 Forbidden.

## US-029: Email Notifications for Key Events (F8) — planned

As Developer, I want to receive emails when my PRs are reviewed or CI fails, so that I can stay informed without constantly checking the dashboard.

Acceptance criteria:
- [ ] Given a PR is opened, when the event is processed, then an email is sent to the assigned reviewers.
- [ ] Given a pipeline fails, when the run completes, then an email is sent to the PR author.
- [ ] Given I mention a user in a comment, when the comment is posted, then an email is sent to the mentioned user.

## US-030: Configure Outgoing Webhooks (F8) — planned

As DevOps Engineer, I want to configure webhooks to send events to external tools, so that I can integrate Quill with Slack or ITSM.

Acceptance criteria:
- [ ] Given I am a repo admin, when I add a webhook URL for 'PR Opened', then the webhook is saved.
- [ ] Given a PR is opened, when the event occurs, then Quill sends a POST request to the configured webhook URL with a JSON payload containing PR details.
- [ ] Given a webhook endpoint returns 500, when Quill retries, then it attempts delivery 3 times with exponential backoff.

## US-031: REST API for Access Management (F8) — planned

As External ITSM Tool, I want to grant and revoke access via API, so that I can automate access provisioning from my ticketing system.

Acceptance criteria:
- [ ] Given a valid API token, when I POST /api/v1/projects/{id}/members, then the user is granted access.
- [ ] Given a valid API token, when I DELETE /api/v1/projects/{id}/members/{user_id}, then the user's access is revoked.
- [ ] Given an invalid API token, when I attempt to modify access, then the system returns 401 Unauthorized.

## US-032: Trigger Pipelines via API (F8) — planned

As External CI Orchestrator, I want to trigger a pipeline run via API, so that I can start builds from external systems.

Acceptance criteria:
- [ ] Given a valid API token, when I POST /api/v1/projects/{id}/pipelines/{pipeline_id}/runs, then a new pipeline run is created and returns a 202 Accepted.
- [ ] Given an invalid pipeline ID, when I attempt to trigger a run, then the system returns 404 Not Found.

## US-033: Public Landing Page (F9) — exists

As Prospective Customer, I want to view a marketing site for Quill, so that I can understand the product value proposition.

Acceptance criteria:
- [ ] Given I am not logged in, when I visit the root URL, then I see a landing page with hero section, features, and testimonials.
- [ ] Given I am logged in, when I visit the root URL, then I am redirected to the dashboard.
- [ ] Given I am on the landing page, when I click 'Sign Up', then I am taken to the registration form.

## US-034: Pricing Page with Tier Details (F9) — exists

As Prospective Customer, I want to see the pricing tiers and limits, so that I can decide which plan fits my needs.

Acceptance criteria:
- [ ] Given I am on the pricing page, when I view the 'Individual' card, then I see 'Free' with limits: 50 repos, 1GB storage, 100 min/mo.
- [ ] Given I am on the pricing page, when I view the 'Enterprise' card, then I see 'Contact Sales' and a description of usage-based billing.
- [ ] Given I click 'Get Started' on the Individual card, when I am not logged in, then I am taken to the registration page.
