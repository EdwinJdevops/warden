# Warden

Closed-loop Kubernetes policy remediation with a hard-gated autonomy boundary.

## Status: Week 1, early build. Not production-ready. Read this section before anything else.

This repo is honest about what exists and what does not. Nothing here claims
to be more finished than it is.

**Built and verified:**
- Two Kyverno ClusterPolicies (`manifests/policies/`), Audit mode.
- Paired violation/compliant sample manifests for the resource-limits policy.
- The remediation tier classifier (`controller/internal/remediation/tier.go`)
  with a full unit test suite. This is the safety-critical piece: it decides
  whether a violation can be auto-merged or must be held for human approval.
  All 7 tests pass, `go vet` clean, `gofmt` clean.

**Not built yet:**
- The Kubernetes controller itself (the reconciliation loop that watches for
  Kyverno PolicyReports and calls the classifier). This needs `client-go` and
  `controller-runtime`, which could not be fetched in the build environment
  used to scaffold this repo. It compiles and gets tested on a machine with
  normal internet access, not here.
- GitOps PR generation logic.
- The audit trail service.
- The Terraform AKS module (used once for a cloud portability proof run,
  then torn down).

## Why this exists

Policy engines (Kyverno, OPA Gatekeeper) block or flag bad manifests. They
do not fix them. Drift and scanning tools report violations and stop at a
diff, a human still has to read it, judge the risk, and merge the fix. That
creates a backlog that grows faster than review capacity.

Kyverno's own `mutate` policies can patch live objects at admission time,
but that fixes the cluster, not the git source, which breaks GitOps's
single-source-of-truth model. Warden's actual position: fix the git source,
merge through the normal pipeline, keep git authoritative.

## The core design decision

Not every violation deserves the same trust level. Warden classifies each
policy into one of two tiers, declared directly on the ClusterPolicy as an
annotation, not hidden in application code:

- `warden.io/remediation-tier: "auto"`. Low blast radius (missing resource
  limits, wrong labels). Eligible for auto-merge if the computed confidence
  score clears the policy's declared baseline.
- `warden.io/remediation-tier: "manual-gate"`. High blast radius (prod
  network policy, RBAC). PR opens, auto-merge is forbidden unconditionally,
  regardless of confidence score. See `TestManualGateNeverAutoMerges` in
  `controller/internal/remediation/tier_test.go`, this is the test that
  guarantees that boundary can't be silently broken by a future refactor.

## Repo layout

```
warden/
├── manifests/
│   ├── policies/      Kyverno ClusterPolicies
│   ├── violations/     sample non-compliant workloads
│   └── compliant/      the target state Warden's remediation converges toward
├── controller/
│   ├── cmd/warden/     entrypoint (not yet implemented)
│   └── internal/
│       └── remediation/  tier classifier, fully tested
├── terraform/           AKS proof-run module (not yet implemented)
└── docs/
    └── ARCHITECTURE.md
```

## Running what exists today

```bash
# Tier classifier tests, stdlib only, no cluster needed
cd controller/internal/remediation
go test -v ./...
```

## Running the Kyverno policies (needs kind + Kyverno installed)

```bash
kind create cluster --name warden-dev
kubectl apply -f https://github.com/kyverno/kyverno/releases/download/v1.12.0/install.yaml
kubectl wait --for=condition=Ready pods --all -n kyverno --timeout=120s

kubectl apply -f manifests/policies/require-resource-limits.yaml
kubectl apply -f manifests/violations/no-resource-limits.yaml
kubectl get policyreport -o yaml   # should show a FAIL against legacy-worker
```

That last step has not been run against a real cluster yet from this
machine. It is the first thing to verify on yours.

## License

MIT, see LICENSE.
