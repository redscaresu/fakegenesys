variable "content" {
  type = string
}

resource "genesyscloud_responsemanagement_library" "lib" {
  name = "msg-library"
}

resource "genesyscloud_responsemanagement_response" "msg" {
  name        = "msg-updates"
  library_ids = [genesyscloud_responsemanagement_library.lib.id]

  texts {
    content      = var.content
    content_type = "text/plain"
  }
}
