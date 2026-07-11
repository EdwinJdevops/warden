package gitops

import (
	"strings"
	"testing"
)

const violatingManifest = `apiVersion: v1
kind: Pod
metadata:
  name: legacy-worker
  namespace: default
  labels:
    app: legacy-worker
spec:
  containers:
    - name: worker
      image: busybox:1.36
      command: ["sleep", "3600"]
`

func TestGeneratePatchesMissingResources(t *testing.T) {
	p := ResourceLimitsPatcher{}
	out, err := p.Generate(violatingManifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"resources:", "requests:", "limits:", "cpu:", "memory:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected patched output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGeneratePreservesExistingFields(t *testing.T) {
	p := ResourceLimitsPatcher{}
	out, err := p.Generate(violatingManifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"legacy-worker", "busybox:1.36", "sleep"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected patched output to still contain original field %q, got:\n%s", want, out)
		}
	}
}

func TestGenerateSkipsContainerThatAlreadyHasResources(t *testing.T) {
	compliant := `apiVersion: v1
kind: Pod
metadata:
  name: legacy-worker
spec:
  containers:
    - name: worker
      image: busybox:1.36
      resources:
        requests:
          cpu: "50m"
          memory: "32Mi"
        limits:
          cpu: "100m"
          memory: "64Mi"
`
	p := ResourceLimitsPatcher{}
	_, err := p.Generate(compliant)
	if err == nil {
		t.Error("expected error when no container is missing a resources block, got nil")
	}
}

func TestGenerateRejectsManifestWithNoSpec(t *testing.T) {
	p := ResourceLimitsPatcher{}
	_, err := p.Generate("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")
	if err == nil {
		t.Error("expected error for manifest with no spec.containers, got nil")
	}
}

func TestGenerateRejectsInvalidYAML(t *testing.T) {
	p := ResourceLimitsPatcher{}
	_, err := p.Generate("{ this is not: valid yaml: [[[")
	if err == nil {
		t.Error("expected error for invalid YAML input, got nil")
	}
}

func TestGenerateUsesCustomDefaults(t *testing.T) {
	p := ResourceLimitsPatcher{
		DefaultCPURequest:   "200m",
		DefaultMemoryRequest: "128Mi",
		DefaultCPULimit:     "500m",
		DefaultMemoryLimit:   "256Mi",
	}
	out, err := p.Generate(violatingManifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"200m", "128Mi", "500m", "256Mi"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected custom default %q in output, got:\n%s", want, out)
		}
	}
}

func TestGenerateHandlesDeploymentPodTemplate(t *testing.T) {
	deployment := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: legacy-deploy
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: worker
          image: busybox:1.36
`
	p := ResourceLimitsPatcher{}
	out, err := p.Generate(deployment)
	if err != nil {
		t.Fatalf("unexpected error for Deployment-shaped manifest: %v", err)
	}
	if !strings.Contains(out, "resources:") {
		t.Errorf("expected resources block in patched Deployment output, got:\n%s", out)
	}
}
