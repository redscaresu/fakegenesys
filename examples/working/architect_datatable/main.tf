resource "genesyscloud_architect_datatable" "phone_lookup" {
  name        = "phone-lookup"
  description = "Phone number → routing config"

  properties {
    name = "phone"
    type = "string"
  }

  properties {
    name = "queueId"
    type = "string"
  }
}
