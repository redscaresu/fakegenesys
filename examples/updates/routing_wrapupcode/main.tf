variable "description" {
  type = string
}

resource "genesyscloud_routing_wrapupcode" "resolved" {
  name        = "resolved-updates"
  description = var.description
}
