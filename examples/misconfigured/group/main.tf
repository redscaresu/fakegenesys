// Missing required `name` attribute — fakegenesys returns 400.
resource "genesyscloud_group" "no_name" {
  type = "official"
}
