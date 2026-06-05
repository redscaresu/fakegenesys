variable "content" {
  type = string
}

resource "genesyscloud_responsemanagement_response" "msg" {
  name = "msg-updates"
  texts {
    content      = var.content
    content_type = "text/plain"
  }
}
