// Package remediation contains the tier-classification logic that decides
// whether a flagged policy violation is eligible for automatic PR merge
// or must be held for human approval.
//
// Design intent: the classification decision must be readable from the
// ClusterPolicy object itself (via annotations), not buried in controller
// code, so a security reviewer auditing the cluster can see the
// auto-merge boundary without reading Go source.
package remediation

import (
	"fmt"
	"strconv"
)

// Tier represents the remediation autonomy level assigned to a violation.
type Tier string

const (
	// TierAuto means Warden may generate and auto-merge a remediation PR
	// without human approval, provided the computed confidence score
	// clears the policy's declared baseline.
	TierAuto Tier = "auto"

	// TierManualGate means Warden must open the remediation PR but is
	// forbidden from merging it under any confidence score. This is a
	// hard boundary, not a threshold. See Classify below.
	TierManualGate Tier = "manual-gate"

	// TierUnknown is returned when a policy is missing the required
	// warden.io/remediation-tier annotation. Unknown is always treated
	// as manual-gate by the caller; there is no silent default to auto.
	TierUnknown Tier = "unknown"
)

// PolicyAnnotations is the minimal set of fields Classify needs from a
// Kyverno ClusterPolicy's metadata.annotations map. Kept as a plain
// struct (not *unstructured.Unstructured or client-go types) so this
// package has zero external dependencies and stays testable without a
// cluster or a fetched module graph.
type PolicyAnnotations struct {
	PolicyName            string
	RemediationTier       string // raw value of warden.io/remediation-tier
	ConfidenceBaselineRaw string // raw value of warden.io/confidence-baseline, may be "n/a"
}

// Decision is the classifier's output: the resolved tier, whether
// auto-merge is currently permitted for this specific violation, and the
// human-readable reason. The reason string is what gets written to the
// audit trail, so it must stand on its own without the caller adding
// context.
type Decision struct {
	Tier             Tier
	AutoMergeAllowed bool
	ConfidenceScore  float64 // 0 if not applicable (manual-gate / unknown)
	Reason           string
}

// Classify resolves a policy's declared tier and, for auto-tier policies,
// checks the computed confidence score against the policy's declared
// baseline. observedConfidence is supplied by the caller (the
// remediation generator), computed per-violation. This function makes
// no assumptions about how that score was derived; it only enforces the
// gate.
//
// Hard rule: TierManualGate can NEVER return AutoMergeAllowed = true,
// regardless of observedConfidence. This is intentional and tested in
// tier_test.go (TestManualGateNeverAutoMerges) specifically to prevent a
// future refactor from accidentally wiring confidence scoring into the
// manual-gate path.
func Classify(pa PolicyAnnotations, observedConfidence float64) (Decision, error) {
	if pa.PolicyName == "" {
		return Decision{}, fmt.Errorf("remediation: PolicyName is required, got empty string")
	}

	switch pa.RemediationTier {
	case string(TierAuto):
		baseline, err := strconv.ParseFloat(pa.ConfidenceBaselineRaw, 64)
		if err != nil {
			return Decision{
				Tier:   TierAuto,
				Reason: fmt.Sprintf("policy %q is tier=auto but confidence-baseline %q is not a valid float, treating as manual-gate until fixed", pa.PolicyName, pa.ConfidenceBaselineRaw),
			}, nil
		}
		if observedConfidence < 0 || observedConfidence > 1 {
			return Decision{}, fmt.Errorf("remediation: observedConfidence %f out of range [0,1]", observedConfidence)
		}
		allowed := observedConfidence >= baseline
		reason := fmt.Sprintf(
			"policy %q tier=auto: observed confidence %.2f vs baseline %.2f -> auto-merge=%t",
			pa.PolicyName, observedConfidence, baseline, allowed,
		)
		return Decision{
			Tier:             TierAuto,
			AutoMergeAllowed: allowed,
			ConfidenceScore:  observedConfidence,
			Reason:           reason,
		}, nil

	case string(TierManualGate):
		return Decision{
			Tier:             TierManualGate,
			AutoMergeAllowed: false,
			ConfidenceScore:  observedConfidence,
			Reason: fmt.Sprintf(
				"policy %q tier=manual-gate: PR opened, auto-merge forbidden unconditionally regardless of confidence score",
				pa.PolicyName,
			),
		}, nil

	default:
		return Decision{
			Tier:             TierUnknown,
			AutoMergeAllowed: false,
			Reason: fmt.Sprintf(
				"policy %q has missing or unrecognized warden.io/remediation-tier annotation (%q), defaulting to manual-gate, not auto",
				pa.PolicyName, pa.RemediationTier,
			),
		}, nil
	}
}
