package forgejo

import (
	"context"
	"net/http"
	"net/url"
)

// This file wraps Forgejo's branch-protection surface. Quill owns branch
// policies in Postgres and mirrors them here so the git layer blocks direct
// pushes, force-pushes, and deletions of a protected branch, and enforces the
// required-approval gate even for pushes that don't go through Quill's API.

// BranchProtection is a Forgejo branch-protection rule. Only the fields Quill
// manages are modelled; Forgejo fills the rest with defaults.
type BranchProtection struct {
	BranchName             string `json:"branch_name"`
	RuleName               string `json:"rule_name"`
	EnablePush             bool   `json:"enable_push"`
	RequiredApprovals      int64  `json:"required_approvals"`
	DismissStaleApprovals  bool   `json:"dismiss_stale_approvals"`
	BlockOnRejectedReviews bool   `json:"block_on_rejected_reviews"`
	BlockOnOutdatedBranch  bool   `json:"block_on_outdated_branch"`
}

// BranchProtectionOptions is the payload to create or edit a branch-protection
// rule. enable_push=false makes the branch mergeable only via pull request.
type BranchProtectionOptions struct {
	BranchName             string `json:"branch_name,omitempty"`
	EnablePush             bool   `json:"enable_push"`
	RequiredApprovals      int64  `json:"required_approvals"`
	DismissStaleApprovals  bool   `json:"dismiss_stale_approvals"`
	BlockOnRejectedReviews bool   `json:"block_on_rejected_reviews"`
	BlockOnOutdatedBranch  bool   `json:"block_on_outdated_branch"`
}

// GetBranchProtection returns the protection rule for a branch. ok is false when
// no rule exists (Forgejo returns 404).
func (c *Client) GetBranchProtection(ctx context.Context, owner, repo, branch string) (BranchProtection, bool, error) {
	var out BranchProtection
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branch_protections/" + url.PathEscape(branch)
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	if NotFound(err) {
		return BranchProtection{}, false, nil
	}
	if err != nil {
		return BranchProtection{}, false, err
	}
	return out, true, nil
}

// UpsertBranchProtection creates the rule for opts.BranchName, or edits it in
// place when one already exists.
func (c *Client) UpsertBranchProtection(ctx context.Context, owner, repo string, opts BranchProtectionOptions) error {
	branch := opts.BranchName
	_, exists, err := c.GetBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		return err
	}
	base := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branch_protections"
	if exists {
		// Editing does not take branch_name in the body.
		edit := opts
		edit.BranchName = ""
		return c.do(ctx, http.MethodPatch, base+"/"+url.PathEscape(branch), edit, nil)
	}
	return c.do(ctx, http.MethodPost, base, opts, nil)
}

// DeleteBranchProtection removes a branch's protection rule. A missing rule is
// treated as success.
func (c *Client) DeleteBranchProtection(ctx context.Context, owner, repo, branch string) error {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branch_protections/" + url.PathEscape(branch)
	err := c.do(ctx, http.MethodDelete, p, nil, nil)
	if NotFound(err) {
		return nil
	}
	return err
}
