# Warden AKS proof-run module.
#
# Purpose: prove the Kyverno policy layer (both ClusterPolicies, already
# verified working on local kind) also works unmodified on a managed
# cloud Kubernetes control plane, demonstrating Warden is not tied to
# one vendor's implementation of Kubernetes.
#
# This module intentionally does NOT provision anything beyond a single
# small node AKS cluster. The Warden controller does not exist yet at
# the time this module was written, there is nothing else to prove on
# AKS yet. Do not expand this module's scope until the controller is
# built and tested on kind first.
#
# Lifecycle: apply, run the same kubectl verification steps already
# proven on kind, capture the output, terraform destroy the same day.
# This is deliberate, not a shortcut, idle AKS spend with no offsetting
# purpose is a FinOps failure, not a demonstration of cloud skill.

resource "azurerm_resource_group" "warden" {
  name     = var.resource_group_name
  location = var.location

  tags = {
    project   = "warden"
    purpose   = "one-time-cloud-portability-proof"
    lifecycle = "ephemeral-destroy-same-day"
  }
}

resource "azurerm_kubernetes_cluster" "warden" {
  name                = var.cluster_name
  location            = azurerm_resource_group.warden.location
  resource_group_name = azurerm_resource_group.warden.name
  dns_prefix          = var.dns_prefix

  default_node_pool {
    name       = "system"
    node_count = var.node_count
    vm_size    = var.node_vm_size
  }

  identity {
    type = "SystemAssigned"
  }

  tags = {
    project   = "warden"
    purpose   = "one-time-cloud-portability-proof"
    lifecycle = "ephemeral-destroy-same-day"
  }
}
