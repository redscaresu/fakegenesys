variable "description" {
  type = string
}

resource "genesyscloud_architect_datatable" "table" {
  name        = "lookup-updates"
  description = var.description

  properties {
    name = "lookup_key"
    type = "string"
  }
}
