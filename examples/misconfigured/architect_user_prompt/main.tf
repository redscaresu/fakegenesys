// Duplicate name → 409.
resource "genesyscloud_architect_user_prompt" "first" {
  name = "dup-prompt-misconfigured"
}

resource "genesyscloud_architect_user_prompt" "second" {
  name       = "dup-prompt-misconfigured"
  depends_on = [genesyscloud_architect_user_prompt.first]
}
