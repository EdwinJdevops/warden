package remediation

import "testing"

func TestAutoTierMergesAboveBaseline(t *testing.T) {
	pa := PolicyAnnotations{
		PolicyName:            "require-resource-limits",
		RemediationTier:       "auto",
		ConfidenceBaselineRaw: "0.95",
	}
	d, err := Classify(pa, 0.98)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.AutoMergeAllowed {
		t.Errorf("expected AutoMergeAllowed=true when observed 0.98 >= baseline 0.95, got false. Reason: %s", d.Reason)
	}
	if d.Tier != TierAuto {
		t.Errorf("expected Tier=auto, got %s", d.Tier)
	}
}

func TestAutoTierBlocksBelowBaseline(t *testing.T) {
	pa := PolicyAnnotations{
		PolicyName:            "require-resource-limits",
		RemediationTier:       "auto",
		ConfidenceBaselineRaw: "0.95",
	}
	d, err := Classify(pa, 0.80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.AutoMergeAllowed {
		t.Errorf("expected AutoMergeAllowed=false when observed 0.80 < baseline 0.95, got true")
	}
}

// This is the load-bearing test in this package. The manual-gate tier
// exists specifically to guarantee a class of violations (prod network
// policy, RBAC, anything blast-radius-high) can never be auto-merged.
// If this test ever fails, the safety boundary described in the
// project's own README and pitch has been silently broken.
func TestManualGateNeverAutoMerges(t *testing.T) {
	pa := PolicyAnnotations{
		PolicyName:      "restrict-prod-network-policy-bypass",
		RemediationTier: "manual-gate",
	}
	// Sweep confidence from 0.0 to 1.0. Even a perfect 1.0 score must
	// not flip AutoMergeAllowed to true for a manual-gate policy.
	for conf := 0.0; conf <= 1.0; conf += 0.1 {
		d, err := Classify(pa, conf)
		if err != nil {
			t.Fatalf("unexpected error at confidence=%.2f: %v", conf, err)
		}
		if d.AutoMergeAllowed {
			t.Fatalf("SAFETY VIOLATION: manual-gate policy returned AutoMergeAllowed=true at confidence=%.2f", conf)
		}
	}
}

func TestUnknownTierDefaultsToManualGate(t *testing.T) {
	pa := PolicyAnnotations{
		PolicyName:      "some-new-policy-missing-annotation",
		RemediationTier: "",
	}
	d, err := Classify(pa, 0.99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.AutoMergeAllowed {
		t.Errorf("expected unknown tier to default to no-auto-merge, got AutoMergeAllowed=true")
	}
	if d.Tier != TierUnknown {
		t.Errorf("expected Tier=unknown, got %s", d.Tier)
	}
}

func TestEmptyPolicyNameIsError(t *testing.T) {
	_, err := Classify(PolicyAnnotations{}, 0.5)
	if err == nil {
		t.Error("expected error for empty PolicyName, got nil")
	}
}

func TestMalformedConfidenceBaselineFailsSafe(t *testing.T) {
	pa := PolicyAnnotations{
		PolicyName:            "broken-policy",
		RemediationTier:       "auto",
		ConfidenceBaselineRaw: "n/a", // e.g. a manual-gate policy misconfigured with tier=auto
	}
	d, err := Classify(pa, 0.99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.AutoMergeAllowed {
		t.Errorf("expected fail-safe to manual-gate when confidence-baseline is unparseable, got AutoMergeAllowed=true")
	}
}

func TestObservedConfidenceOutOfRangeIsError(t *testing.T) {
	pa := PolicyAnnotations{
		PolicyName:            "require-resource-limits",
		RemediationTier:       "auto",
		ConfidenceBaselineRaw: "0.95",
	}
	if _, err := Classify(pa, 1.5); err == nil {
		t.Error("expected error for observedConfidence > 1, got nil")
	}
	if _, err := Classify(pa, -0.1); err == nil {
		t.Error("expected error for observedConfidence < 0, got nil")
	}
}
