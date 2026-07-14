// Package gitops handles the last step of Warden's remediation loop:
// taking a classified violation and turning it into a real, auditable
// change against the source git repository, not a live cluster patch.
//
// SAFETY BOUNDARY, READ BEFORE MODIFYING:
// OpenRemediationPR NEVER merges anything itself. It only opens a PR.
// MergeIfAllowed is the only function in this package permitted to call
// GitHub's merge endpoint, and it does so only when passed a
// remediation.Decision where AutoMergeAllowed is true. That value is
// computed exclusively by the already-tested tier classifier
// (controller/internal/remediation/tier.go). This package trusts that
// decision, it does not recompute or override it. If you are tempted to
// add a bypass, a manual override flag, or a "just this once" merge
// path here, don't, that is exactly the failure mode the manual-gate
// tier exists to prevent.
package gitops

import (
	"context"
	"fmt"

	"github.com/google/go-github/v62/github"
)

// PatchGenerator produces the file content changes for a given
// violation. Kept as an interface so the resource-limits patch logic
// (deterministic, rule-based) and any future, more complex patch types
// can be swapped without touching the PR-opening logic below.
type PatchGenerator interface {
	// Generate returns the full new file content for path, given the old
	// content. Returns an error if the violation type isn't one this
	// generator knows how to fix, callers should skip that violation
	// rather than guess.
	Generate(oldContent string) (newContent string, err error)
}

// PRRequest describes everything needed to open one remediation PR.
type PRRequest struct {
	Owner      string
	Repo       string
	BaseBranch string // e.g. "main"
	FilePath   string // path within the repo to the manifest being fixed
	CommitMsg  string
	PRTitle    string
	PRBody     string
	AutoMerge  bool // must come from remediation.Decision.AutoMergeAllowed, never set ad hoc
}

// Client wraps the GitHub API client with Warden's PR workflow.
type Client struct {
	gh *github.Client
}

// NewClient builds a gitops.Client authenticated with a personal access
// token or GitHub App installation token. Token is read by the caller,
// this package never reads environment variables itself, keeping
// credential handling explicit at the call site.
func NewClient(token string) *Client {
	return &Client{
		gh: github.NewClient(nil).WithAuthToken(token),
	}
}

// OpenRemediationPR creates a branch, commits the patched file, and opens
// a PR. It never merges. AutoMerge on the request is recorded in the PR
// body for audit purposes but has no effect in this function, the
// caller must explicitly call MergeIfAllowed afterward if merging is
// warranted.
func (c *Client) OpenRemediationPR(ctx context.Context, req PRRequest, branchName string, oldContent, newContent, sha string) (*github.PullRequest, error) {
	if req.Owner == "" || req.Repo == "" || req.BaseBranch == "" {
		return nil, fmt.Errorf("gitops: Owner, Repo, and BaseBranch are required, got %+v", req)
	}

	baseRef, _, err := c.gh.Git.GetRef(ctx, req.Owner, req.Repo, "refs/heads/"+req.BaseBranch)
	if err != nil {
		return nil, fmt.Errorf("gitops: failed to get base ref %q: %w", req.BaseBranch, err)
	}

	newRef := &github.Reference{
		Ref:    github.String("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: baseRef.Object.SHA},
	}
	if _, _, err := c.gh.Git.CreateRef(ctx, req.Owner, req.Repo, newRef); err != nil {
		return nil, fmt.Errorf("gitops: failed to create branch %q: %w", branchName, err)
	}

	fileOpts := &github.RepositoryContentFileOptions{
		Message: github.String(req.CommitMsg),
		Content: []byte(newContent),
		Branch:  github.String(branchName),
		SHA:     github.String(sha),
	}
	if _, _, err := c.gh.Repositories.UpdateFile(ctx, req.Owner, req.Repo, req.FilePath, fileOpts); err != nil {
		return nil, fmt.Errorf("gitops: failed to commit patch to %q on branch %q: %w", req.FilePath, branchName, err)
	}

	body := req.PRBody
	if req.AutoMerge {
		body += "\n\n---\nWarden tier: auto. This PR is eligible for automatic merge based on the confidence-gated tier classifier. See controller/internal/remediation/tier.go."
	} else {
		body += "\n\n---\nWarden tier: manual-gate. This PR requires human review and will NOT be auto-merged, regardless of confidence score."
	}

	newPR := &github.NewPullRequest{
		Title: github.String(req.PRTitle),
		Head:  github.String(branchName),
		Base:  github.String(req.BaseBranch),
		Body:  github.String(body),
	}

	pr, _, err := c.gh.PullRequests.Create(ctx, req.Owner, req.Repo, newPR)
	if err != nil {
		return nil, fmt.Errorf("gitops: failed to open PR from branch %q: %w", branchName, err)
	}

	return pr, nil
}

// GetFileContent fetches the current content and SHA of a file from the
// given branch. The SHA is required by OpenRemediationPR's UpdateFile
// call, GitHub's content API is optimistic-locking based, an update
// without the current SHA is rejected.
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) (content string, sha string, err error) {
	fileContent, _, _, err := c.gh.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		return "", "", fmt.Errorf("gitops: failed to fetch %q from %s/%s@%s: %w", path, owner, repo, ref, err)
	}
	if fileContent == nil {
		return "", "", fmt.Errorf("gitops: %q resolved to a directory, not a file", path)
	}

	decoded, err := fileContent.GetContent()
	if err != nil {
		return "", "", fmt.Errorf("gitops: failed to decode content of %q: %w", path, err)
	}

	return decoded, fileContent.GetSHA(), nil
}

// MergeIfAllowed merges the given PR, but only if allowed is true. This
// is the single chokepoint for all auto-merge activity in the codebase.
// Callers must pass decision.AutoMergeAllowed directly, computed by
// remediation.Classify, not a locally recomputed or hardcoded value.
func (c *Client) MergeIfAllowed(ctx context.Context, owner, repo string, prNumber int, allowed bool, reason string) error {
	if !allowed {
		return fmt.Errorf("gitops: refusing to merge PR #%d, auto-merge not allowed (reason: %s)", prNumber, reason)
	}

	result, _, err := c.gh.PullRequests.Merge(ctx, owner, repo, prNumber, reason, &github.PullRequestOptions{
		MergeMethod: "squash",
	})
	if err != nil {
		return fmt.Errorf("gitops: failed to merge PR #%d: %w", prNumber, err)
	}
	if !result.GetMerged() {
		return fmt.Errorf("gitops: merge of PR #%d reported not merged: %s", prNumber, result.GetMessage())
	}

	return nil
}
