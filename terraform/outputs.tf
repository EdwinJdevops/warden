output "kube_config_raw" {
  description = "Raw kubeconfig for the proof-run cluster. Sensitive, do not commit or print this to a shared log."
  value       = azurerm_kubernetes_cluster.warden.kube_config_raw
  sensitive   = true
}

output "cluster_name" {
  value = azurerm_kubernetes_cluster.warden.name
}

output "resource_group_name" {
  value = azurerm_resource_group.warden.name
}
