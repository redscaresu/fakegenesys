resource "genesyscloud_responsemanagement_library" "greetings" {
  name = "greetings-library"
}

resource "genesyscloud_responsemanagement_response" "canned" {
  name        = "canned-greeting"
  library_ids = [genesyscloud_responsemanagement_library.greetings.id]

  texts {
    content      = "Hello, thanks for calling."
    content_type = "text/plain"
  }
}
