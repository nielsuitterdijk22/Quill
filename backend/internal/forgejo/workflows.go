package forgejo

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// This file adds the read helpers Quill's pipelines layer needs: listing the
// workflow files under .github/workflows and fetching a single workflow's
// decoded YAML. Both build on the generic contents endpoint (GetContents) and,
// like every other read, run with the admin token (visibility is enforced in
// the platform layer).

// workflowsDir is the conventional location of GitHub Actions-style workflows.
const workflowsDir = ".github/workflows"

// maxWorkflowBytes caps the size of a single workflow file Quill will decode.
const maxWorkflowBytes = 512 << 10 // 512 KiB

// WorkflowFile is a workflow definition discovered in a repository.
type WorkflowFile struct {
	// Path is repo-relative, e.g. ".github/workflows/ci.yml".
	Path string
	// Name is the file's base name, e.g. "ci.yml".
	Name string
	SHA  string
}

// ListWorkflows returns the YAML files under .github/workflows at ref (empty ref
// = default branch). A missing directory yields an empty list, not an error, so
// repositories without CI are handled gracefully by callers.
func (c *Client) ListWorkflows(ctx context.Context, owner, repo, ref string) ([]WorkflowFile, error) {
	contents, err := c.GetContents(ctx, owner, repo, workflowsDir, ref)
	if err != nil {
		if NotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if contents == nil || !contents.IsDir {
		return nil, nil
	}
	var out []WorkflowFile
	for _, e := range contents.Entries {
		if e.Type != "file" || !isWorkflowFile(e.Name) {
			continue
		}
		out = append(out, WorkflowFile{Path: e.Path, Name: e.Name, SHA: e.SHA})
	}
	return out, nil
}

// GetWorkflowContent fetches and decodes a single workflow file's YAML at ref.
func (c *Client) GetWorkflowContent(ctx context.Context, owner, repo, path, ref string) (string, error) {
	contents, err := c.GetContents(ctx, owner, repo, path, ref)
	if err != nil {
		return "", err
	}
	if contents == nil || contents.File == nil || contents.File.Content == nil {
		return "", fmt.Errorf("workflow %q has no content", path)
	}
	if contents.File.Encoding == nil || *contents.File.Encoding != "base64" {
		// Forgejo returns base64 for file contents; anything else is unexpected.
		return *contents.File.Content, nil
	}
	raw, err := base64.StdEncoding.DecodeString(*contents.File.Content)
	if err != nil {
		return "", fmt.Errorf("decode workflow %q: %w", path, err)
	}
	if len(raw) > maxWorkflowBytes {
		raw = raw[:maxWorkflowBytes]
	}
	return string(raw), nil
}

// isWorkflowFile reports whether a file name is a YAML workflow definition.
func isWorkflowFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}
