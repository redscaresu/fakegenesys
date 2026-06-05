variable "title" {
  type = string
}

resource "genesyscloud_user" "alice" {
  name  = "Alice Example"
  email = "alice-updates@example.com"
  title = var.title
}
