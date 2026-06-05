// routing/languages doesn't support PUT/PATCH (spec-declared).
// This dir tests no-drift after recreate (replace_triggered_by would
// be the production pattern, but the smoke harness keeps it simple by
// not changing the name across v1/v2).
variable "suffix" {
  type = string
}

resource "genesyscloud_routing_language" "lang" {
  name = "fr-FR-updates-${var.suffix}"
}
