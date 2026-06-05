// Duplicate language name → 409.
resource "genesyscloud_routing_language" "first" {
  name = "duplicate-language-misconfigured"
}

resource "genesyscloud_routing_language" "second" {
  name       = "duplicate-language-misconfigured"
  depends_on = [genesyscloud_routing_language.first]
}
