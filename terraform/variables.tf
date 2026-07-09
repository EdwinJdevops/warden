variable "resource_group_name" {
  description = "Resource group for the one-time Warden AKS proof run. Destroyed same day as apply."
  type        = string
  default     = "warden-proof-run-rg"
}

variable "location" {
  description = "Azure region. Pick one close to you to reduce latency during the live test."
  type        = string
  default     = "westeurope"
}

variable "cluster_name" {
  description = "AKS cluster name."
  type        = string
  default     = "warden-proof-run"
}

variable "dns_prefix" {
  description = "DNS prefix for the cluster's API server."
  type        = string
  default     = "wardenproof"
}

variable "node_count" {
  description = "Node count for the system pool. Kept at 1 deliberately, this proves the policy layer works on AKS, it does not need to be a real multi-node cluster to prove that."
  type        = number
  default     = 1
}

variable "node_vm_size" {
  description = "VM size for the single node. B2s is burstable and cheap, appropriate for a same-day proof run, not for anything meant to stay up."
  type        = string
  default     = "Standard_B2s"
}
