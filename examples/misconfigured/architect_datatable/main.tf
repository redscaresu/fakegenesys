// Missing required `name` → 400.
resource "genesyscloud_architect_datatable" "no_name" {
  description = "no name"
}
