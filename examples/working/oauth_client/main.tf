resource "genesyscloud_oauth_client" "integration_bot" {
  name                  = "integration-bot"
  authorized_grant_type = "CLIENT_CREDENTIALS"
}
