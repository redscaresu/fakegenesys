variable "description" {
  type = string
}

resource "genesyscloud_routing_queue" "support" {
  name        = "support-updates"
  description = var.description
}
