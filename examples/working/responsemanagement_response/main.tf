resource "genesyscloud_responsemanagement_response" "canned" {
  name = "canned-greeting"
  texts {
    content      = "Hello, thanks for calling."
    content_type = "text/plain"
  }
}
