variable "notes" {
  type = string
}

resource "genesyscloud_location" "office" {
  name  = "Office-updates"
  notes = var.notes
}
