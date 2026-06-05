// Missing required `authorized_grant_type` — fakegenesys returns 400.
resource "genesyscloud_oauth_client" "no_grant" {
  name = "no-grant"
}
