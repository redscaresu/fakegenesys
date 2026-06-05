variable "label" {
  type = string
}

resource "genesyscloud_routing_skill" "english" {
  name        = "english-skill-updates"
  description = var.label
}
