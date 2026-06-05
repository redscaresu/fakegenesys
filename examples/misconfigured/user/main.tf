// Missing required `email` attribute — fakegenesys returns 400.
resource "genesyscloud_user" "no_email" {
  name = "Bob NoEmail"
}
