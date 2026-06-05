variable "description" {
  type = string
}

resource "genesyscloud_group" "eng" {
  name        = "Engineering-updates"
  type        = "official"
  description = var.description
}
