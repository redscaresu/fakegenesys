variable "description" {
  type = string
}

resource "genesyscloud_oauth_client" "bot" {
  name                  = "bot-updates"
  description           = var.description
  authorized_grant_type = "CLIENT-CREDENTIALS"
}
