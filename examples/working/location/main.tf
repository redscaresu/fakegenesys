resource "genesyscloud_location" "hq" {
  name = "Headquarters"

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
