// Duplicate skill name → 409.
resource "genesyscloud_routing_skill" "first" {
  name = "duplicate-skill-misconfigured"
}

resource "genesyscloud_routing_skill" "second" {
  name       = "duplicate-skill-misconfigured"
  depends_on = [genesyscloud_routing_skill.first]
}
