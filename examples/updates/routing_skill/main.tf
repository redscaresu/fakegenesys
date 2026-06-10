variable "label" {
  type = string
}

# Provider deprecated `description` on genesyscloud_routing_skill. The
# label variable is kept (so v1/v2.tfvars remain valid input files)
# but is no longer wired into the resource — the updates contract
# (apply → plan-no-op → re-apply → plan-no-op → destroy) holds even
# when v1 and v2 produce identical state.
resource "genesyscloud_routing_skill" "english" {
  name = "english-skill-updates"
}
