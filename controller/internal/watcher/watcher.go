// Package watcher fetches Kyverno PolicyReport and ClusterPolicy objects
// via the Kubernetes dynamic client. The dynamic client is used
// deliberately instead of Kyverno's own generated Go client types
// (kyverno.io/kyverno/pkg/client), to keep this repo's dependency graph
// to just client-go and apimachinery, one fewer module to pull in and
// one fewer place a version mismatch can break the build.
package watcher

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var policyReportGVR = schema.GroupVersionResource{
	Group:    "wgpolicyk8s.io",
	Version:  "v1alpha2",
	Resource: "policyreports",
}

var clusterPolicyGVR = schema.GroupVersionResource{
	Group:    "kyverno.io",
	Version:  "v1",
	Resource: "clusterpolicies",
}

// Violation is a single failing result extracted from a PolicyReport.
type Violation struct {
	Namespace    string
	ResourceName string
	PolicyName   string
	RuleName     string
	Message      string
}

// PolicyAnnotationsRaw mirrors remediation.PolicyAnnotations but is
// defined here to avoid this package importing the remediation package
// just for a struct shape, main.go does the conversion at the call site.
type PolicyAnnotationsRaw struct {
	PolicyName            string
	RemediationTier       string
	ConfidenceBaselineRaw string
}

// FetchFailingViolations lists PolicyReports across all namespaces and
// returns every result with result == "fail". PolicyReports with zero
// failing results are skipped entirely, not returned as empty violations.
func FetchFailingViolations(ctx context.Context, dyn dynamic.Interface) ([]Violation, error) {
	list, err := dyn.Resource(policyReportGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("watcher: failed to list policyreports: %w", err)
	}

	var violations []Violation

	for _, item := range list.Items {
		results, found, err := unstructured.NestedSlice(item.Object, "results")
		if err != nil {
			return nil, fmt.Errorf("watcher: malformed results field on policyreport %s/%s: %w",
				item.GetNamespace(), item.GetName(), err)
		}
		if !found {
			continue
		}

		scopeNamespace := item.GetNamespace()
		scopeName, _, _ := unstructured.NestedString(item.Object, "scope", "name")
		if scopeName == "" {
			scopeName = item.GetName()
		}

		for _, r := range results {
			result, ok := r.(map[string]interface{})
			if !ok {
				continue
			}

			resultStatus, _, _ := unstructured.NestedString(result, "result")
			if resultStatus != "fail" {
				continue
			}

			policyName, _, _ := unstructured.NestedString(result, "policy")
			ruleName, _, _ := unstructured.NestedString(result, "rule")
			message, _, _ := unstructured.NestedString(result, "message")

			if policyName == "" {
				continue
			}

			violations = append(violations, Violation{
				Namespace:    scopeNamespace,
				ResourceName: scopeName,
				PolicyName:   policyName,
				RuleName:     ruleName,
				Message:      message,
			})
		}
	}

	return violations, nil
}

// FetchPolicyAnnotations reads the warden.io/* annotations off the named
// ClusterPolicy (cluster-scoped, no namespace argument needed).
func FetchPolicyAnnotations(ctx context.Context, dyn dynamic.Interface, policyName string) (PolicyAnnotationsRaw, error) {
	obj, err := dyn.Resource(clusterPolicyGVR).Get(ctx, policyName, metav1.GetOptions{})
	if err != nil {
		return PolicyAnnotationsRaw{}, fmt.Errorf("watcher: failed to get clusterpolicy %q: %w", policyName, err)
	}

	annotations := obj.GetAnnotations()

	return PolicyAnnotationsRaw{
		PolicyName:            policyName,
		RemediationTier:       annotations["warden.io/remediation-tier"],
		ConfidenceBaselineRaw: annotations["warden.io/confidence-baseline"],
	}, nil
}
