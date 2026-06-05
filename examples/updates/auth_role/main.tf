variable "description" {
  type = string
}

resource "genesyscloud_auth_role" "supervisor" {
  name        = "supervisor-updates"
  description = var.description
  permissions = [
    "routing:queue:view",
  ]
}
