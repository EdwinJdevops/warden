# Architecture

## Current state (Week 1)

```
Kyverno (Audit mode)
      |
      v
PolicyReport (violation flagged, not blocked)
      |
      v
[ NOT YET BUILT: controller watches PolicyReports ]
      |
      v
tier.Classify(policyAnnotations, confidenceScore)
      |
      +-- tier=auto, confidence >= baseline --> [ NOT YET BUILT: auto-merge PR ]
      |
      +-- tier=manual-gate, always         --> [ NOT YET BUILT: PR held for approval ]
```

The only box in this diagram that is actually implemented and tested today
is `tier.Classify`. Everything else is the plan, not the state.

## Planned data flow (post controller build)

1. Kyverno evaluates admission and background scans, writes PolicyReports.
2. Warden controller watches PolicyReports via `client-go` informers.
3. For each new violation, controller reads the source ClusterPolicy's
   `warden.io/*` annotations, computes a confidence score for the specific
   fix (deterministic, rule-based for the resource-limits case, not an LLM
   call, since a fixed patch like "add resource limits" does not need a
   model to generate), and calls `remediation.Classify`.
4. If `AutoMergeAllowed`, controller opens a PR against the source git repo
   with the patch and immediately merges it via the GitHub API, then
   records the decision (policy, confidence, reason string, PR link) to the
   audit trail service on Render.
5. If not, PR opens, stays open, audit trail records "held for review."
6. ArgoCD picks up the merged commit and syncs it to cluster state as
   normal, no special-casing in the GitOps layer.

## Why confidence scoring is deterministic here, not AI

The `require-resource-limits` violation class has exactly one correct fix
shape: add a `resources` block with sane defaults. There is no ambiguity to
resolve, so a confidence model here would be theater. Confidence in this
context means "how certain are we this specific patch will not break the
workload," computed from things like: is this a stateless deployment,
does it have existing resource usage history to derive sane request/limit
values from, is it in a non-prod namespace. That is closer to a scoring
function than to a model.
