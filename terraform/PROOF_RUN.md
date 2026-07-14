# AKS Proof Run

Purpose: prove the Kyverno policy layer (already verified working on
local kind) also runs unmodified on a managed cloud Kubernetes control
plane. Nothing more. The Warden controller does not exist yet, do not
try to prove anything beyond the policy layer here.

Time-boxed. Azure trial credit is expiring within days at the time this
was written. Apply, verify, destroy, same session if possible.

## Prerequisites

```bash
az login
az account show   # confirm you're on the free trial subscription, not a different one
```

## Apply

```bash
cd terraform
terraform init
terraform plan -out=proof.tfplan
terraform apply proof.tfplan
```

Read the plan output before applying. Confirm it shows exactly one
resource group and one AKS cluster, nothing else. If it shows more,
stop and paste the plan back for review before applying.

## Get cluster access

```bash
az aks get-credentials \
  --resource-group $(terraform output -raw resource_group_name) \
  --name $(terraform output -raw cluster_name) \
  --overwrite-existing

kubectl config current-context   # should now point at the AKS cluster, not kind-warden-dev
```

## Verify, identical steps already proven on kind

```bash
kubectl apply --server-side -f https://github.com/kyverno/kyverno/releases/download/v1.12.0/install.yaml
kubectl wait --for=condition=Ready pods --all -n kyverno --timeout=180s

kubectl apply -f ../manifests/policies/require-resource-limits.yaml
kubectl apply -f ../manifests/policies/restrict-prod-network-policy-bypass.yaml
kubectl apply -f ../manifests/violations/no-resource-limits.yaml

kubectl create namespace test-prod
kubectl label namespace test-prod env=prod
kubectl run test-pod --image=busybox:1.36 -n test-prod --command -- sleep 3600

sleep 30
kubectl get policyreport -A -o yaml > aks-proof-run-evidence.yaml
```

Save `aks-proof-run-evidence.yaml`, this is the artifact that proves
cloud portability, keep it in the repo under `docs/`.

## Destroy, same day, no exceptions

```bash
kubectl config use-context kind-warden-dev   # switch back to local before destroying, avoid confusion
cd terraform
terraform destroy
```

Confirm in the Azure portal afterward that the resource group is
actually gone, not just that Terraform reported success.

```bash
az group show --name $(terraform output -raw resource_group_name 2>/dev/null) || echo "confirmed gone"
```
