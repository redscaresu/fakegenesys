// Missing required `name` → 400.
resource "genesyscloud_responsemanagement_response" "no_name" {
  texts {
    content      = "Body"
    content_type = "text/plain"
  }
}
