variable "description" {
  type = string
}

resource "genesyscloud_architect_user_prompt" "greet" {
  name        = "greet-updates"
  description = var.description
}
