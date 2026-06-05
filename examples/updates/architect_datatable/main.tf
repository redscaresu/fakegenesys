variable "description" {
  type = string
}

resource "genesyscloud_architect_datatable" "table" {
  name        = "lookup-updates"
  description = var.description
  schema      = jsonencode({
    "$schema" : "http://json-schema.org/draft-04/schema#",
    "type" : "object"
  })
}
