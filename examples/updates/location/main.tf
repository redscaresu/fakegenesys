variable "notes" {
  type = string
}

resource "genesyscloud_location" "office" {
  name  = "Office-updates"
  notes = var.notes

  address {
    street1  = "1 Market St"
    city     = "San Francisco"
    zip_code = "94105"
    country  = "US"
  }

  emergency_number {
    number = "+14155550100"
  }
}
