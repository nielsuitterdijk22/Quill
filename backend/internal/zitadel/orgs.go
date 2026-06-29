package zitadel

import "context"

// CreateOrg creates a new Zitadel organisation named name and, when adminUserID
// is set, assigns that user as an org owner. Returns the new organisation id.
// Uses the v2 API, which creates the org and assigns admins in one call.
func (c *Client) CreateOrg(ctx context.Context, name, adminUserID string) (string, error) {
	type admin struct {
		UserID string   `json:"userId"`
		Roles  []string `json:"roles"`
	}
	body := map[string]any{"name": name}
	if adminUserID != "" {
		body["admins"] = []admin{{UserID: adminUserID, Roles: []string{"ORG_OWNER"}}}
	}
	var out struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := c.do(ctx, "POST", "/v2/organizations", "", body, &out); err != nil {
		return "", err
	}
	return out.OrganizationID, nil
}

// OrgMember is a member of an organisation with their assigned roles.
type OrgMember struct {
	UserID      string   `json:"userId"`
	Roles       []string `json:"roles"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"preferredLoginName"`
}

// ListOrgMembers returns the members of orgID via the Management API member
// search, scoped to that org.
func (c *Client) ListOrgMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	var out struct {
		Result []OrgMember `json:"result"`
	}
	if err := c.do(ctx, "POST", "/management/v1/orgs/me/members/_search", orgID, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

// AddOrgMember adds an existing Zitadel user to orgID with the given roles
// (e.g. "ORG_OWNER"). The user must already exist in the instance.
func (c *Client) AddOrgMember(ctx context.Context, orgID, userID string, roles []string) error {
	body := map[string]any{"userId": userID, "roles": roles}
	return c.do(ctx, "POST", "/management/v1/orgs/me/members", orgID, body, nil)
}

// RemoveOrgMember removes a user from orgID.
func (c *Client) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	return c.do(ctx, "DELETE", "/management/v1/orgs/me/members/"+userID, orgID, nil, nil)
}
