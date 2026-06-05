resource "genesyscloud_architect_datatable" "phone_lookup" {
  name        = "phone-lookup"
  description = "Phone number → routing config"
  schema      = jsonencode({
    "$schema" : "http://json-schema.org/draft-04/schema#",
    "type" : "object",
    "properties" : {
      "phone" : { "type" : "string" },
      "queueId" : { "type" : "string" }
    }
  })
}
