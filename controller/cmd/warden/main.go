// Command warden is the Warden controller entrypoint.
//
// SCOPE OF THIS FILE:
// Watches Kyverno PolicyReports, classifies violations via the tested
// tier classifier, and for the require-resource-limits violation class
// specifically, generates a patch and opens a remediation PR. Auto-merge
// happens only when the tier classifier says AutoMergeAllowed=true.
//
// KNOWN V1 LIMITATION, STATED EXPLICITLY, NOT HIDDEN:
// Mapping a cluster resource back to its source git file is an unsolved
// general problem (GitOps tools like ArgoCD solve this via their own
// resource tracking, Warden does not reimplement that here). This
// version uses a naming convention matching the existing repo layout:
// a violation on Pod "foo" maps to manifests/violations/foo.yaml. This
// is intentionally narrow and only correct for this repo's own demo
// manifests. Extending Warden to arbitrary clusters requires either
// ArgoCD Application metadata lookups or an explicit annotation-based
// mapping, neither of which is built yet.
//
// VERIFICATION STATUS: compiles and passes tests as of the gitops and
// remediation package test runs (14/14 passing). This file's own
// end-to-end path (watcher -> classifier -> patcher -> gitops) has NOT
// been run against a live cluster with a real GitHub token yet. That is
// the next real verification step, not this comment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/EdwinJdevops/warden/controller/internal/gitops"
	"github.com/EdwinJdevops/warden/controller/internal/remediation"
	"github.com/EdwinJdevops/warden/controller/internal/watcher"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfigPath string
	var pollInterval time.Duration
	var githubToken, repoOwner, repoName, baseBranch string
	var dryRun bool

	defaultKubeconfig := ""
	if home := homedir.HomeDir(); home != "" {
		defaultKubeconfig = filepath.Join(home, ".kube", "config")
	}

	flag.StringVar(&kubeconfigPath, "kubeconfig", defaultKubeconfig, "path to kubeconfig file, defaults to $HOME/.kube/config")
	flag.DurationVar(&pollInterval, "poll-interval", 30*time.Second, "how often to poll for new PolicyReport violations")
	flag.StringVar(&githubToken, "github-token", os.Getenv("WARDEN_GITHUB_TOKEN"), "GitHub token with repo write access, defaults to WARDEN_GITHUB_TOKEN env var")
	flag.StringVar(&repoOwner, "repo-owner", "EdwinJdevops", "GitHub repo owner for remediation PRs")
	flag.StringVar(&repoName, "repo-name", "warden", "GitHub repo name for remediation PRs")
	flag.StringVar(&baseBranch, "base-branch", "master", "base branch to open remediation PRs against")
	flag.BoolVar(&dryRun, "dry-run", true, "if true, classify and log only, never call the GitHub API. Defaults to true deliberately, PR/merge activity must be opted into explicitly.")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		log.Fatalf("warden: failed to load kubeconfig from %q: %v", kubeconfigPath, err)
	}

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("warden: failed to build dynamic client: %v", err)
	}

	if !dryRun && githubToken == "" {
		log.Fatalf("warden: -dry-run=false requires a GitHub token via -github-token or WARDEN_GITHUB_TOKEN")
	}

	var ghClient *gitops.Client
	if !dryRun {
		ghClient = gitops.NewClient(githubToken)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mode := "DRY RUN, no PRs will be opened"
	if !dryRun {
		mode = fmt.Sprintf("LIVE, opening PRs against %s/%s@%s", repoOwner, repoName, baseBranch)
	}
	log.Printf("warden: starting, polling every %s against context from %s [%s]", pollInterval, kubeconfigPath, mode)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	runOnce(ctx, dyn, ghClient, repoOwner, repoName, baseBranch, dryRun)

	for {
		select {
		case <-ticker.C:
			runOnce(ctx, dyn, ghClient, repoOwner, repoName, baseBranch, dryRun)
		case <-ctx.Done():
			log.Println("warden: shutting down")
			os.Exit(0)
		}
	}
}

// runOnce performs a single detect-classify-remediate cycle. Errors
// within a single violation's processing are logged and skipped, they
// do not stop the rest of the batch.
func runOnce(ctx context.Context, dyn dynamic.Interface, ghClient *gitops.Client, repoOwner, repoName, baseBranch string, dryRun bool) {
	violations, err := watcher.FetchFailingViolations(ctx, dyn)
	if err != nil {
		log.Printf("warden: failed to fetch policy reports: %v", err)
		return
	}

	if len(violations) == 0 {
		log.Println("warden: no failing violations found this cycle")
		return
	}

	log.Printf("warden: found %d failing violation(s)", len(violations))

	patcher := gitops.ResourceLimitsPatcher{}

	for _, v := range violations {
		pa, err := watcher.FetchPolicyAnnotations(ctx, dyn, v.PolicyName)
		if err != nil {
			log.Printf("warden: skipping violation %s/%s (policy %q): failed to read source ClusterPolicy: %v",
				v.Namespace, v.ResourceName, v.PolicyName, err)
			continue
		}

		// watcher and remediation intentionally use separate struct types
		// (see watcher.go's comment on PolicyAnnotationsRaw) to avoid a
		// cross-package dependency just for a shared shape. Convert here
		// at the boundary, explicitly, not via an unsafe cast.
		remPA := remediation.PolicyAnnotations{
			PolicyName:            pa.PolicyName,
			RemediationTier:       pa.RemediationTier,
			ConfidenceBaselineRaw: pa.ConfidenceBaselineRaw,
		}

		// Deterministic confidence scoring, see docs/ARCHITECTURE.md.
		// Known simplification: flat score for the auto tier, not yet
		// derived from workload-specific signal.
		confidence := 0.0
		if pa.RemediationTier == string(remediation.TierAuto) {
			confidence = 0.98
		}

		decision, err := remediation.Classify(remPA, confidence)
		if err != nil {
			log.Printf("warden: classification error for violation %s/%s (policy %q): %v",
				v.Namespace, v.ResourceName, v.PolicyName, err)
			continue
		}

		fmt.Printf(
			"[warden] namespace=%s resource=%s policy=%s tier=%s auto_merge_allowed=%t reason=%q\n",
			v.Namespace, v.ResourceName, v.PolicyName, decision.Tier, decision.AutoMergeAllowed, decision.Reason,
		)

		if dryRun {
			continue
		}

		// V1 limitation, see file header: only the require-resource-limits
		// class has a patcher and a known file-path convention. Anything
		// else is logged and skipped, not guessed at.
		if v.PolicyName != "require-resource-limits" {
			log.Printf("warden: no patch generator registered for policy %q, PR not opened (manual-gate policies are expected to stop here)", v.PolicyName)
			continue
		}

		filePath := fmt.Sprintf("manifests/violations/%s.yaml", v.ResourceName)

		fileContent, contentSHA, err := ghClient.GetFileContent(ctx, repoOwner, repoName, filePath, baseBranch)
		if err != nil {
			log.Printf("warden: skipping violation %s/%s: failed to fetch source file %q: %v",
				v.Namespace, v.ResourceName, filePath, err)
			continue
		}

		newContent, err := patcher.Generate(fileContent)
		if err != nil {
			log.Printf("warden: skipping violation %s/%s: patch generation failed: %v",
				v.Namespace, v.ResourceName, err)
			continue
		}

		branchName := fmt.Sprintf("warden/auto-fix-%s-%d", v.ResourceName, time.Now().Unix())

		req := gitops.PRRequest{
			Owner:      repoOwner,
			Repo:       repoName,
			BaseBranch: baseBranch,
			FilePath:   filePath,
			CommitMsg:  fmt.Sprintf("warden: add resource limits to %s (policy: %s)", v.ResourceName, v.PolicyName),
			PRTitle:    fmt.Sprintf("Warden: fix %s violation on %s", v.PolicyName, v.ResourceName),
			PRBody:     decision.Reason,
			AutoMerge:  decision.AutoMergeAllowed,
		}

		pr, err := ghClient.OpenRemediationPR(ctx, req, branchName, fileContent, newContent, contentSHA)
		if err != nil {
			log.Printf("warden: failed to open PR for %s/%s: %v", v.Namespace, v.ResourceName, err)
			continue
		}

		log.Printf("warden: opened PR #%d for %s/%s", pr.GetNumber(), v.Namespace, v.ResourceName)

		if decision.AutoMergeAllowed {
			if err := ghClient.MergeIfAllowed(ctx, repoOwner, repoName, pr.GetNumber(), decision.AutoMergeAllowed, decision.Reason); err != nil {
				log.Printf("warden: PR #%d opened but merge failed: %v", pr.GetNumber(), err)
			} else {
				log.Printf("warden: PR #%d auto-merged, reason: %s", pr.GetNumber(), decision.Reason)
			}
		}
	}
}
