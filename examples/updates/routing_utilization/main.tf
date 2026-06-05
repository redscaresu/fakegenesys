variable "call_capacity" {
  type = number
}

resource "genesyscloud_routing_utilization" "org" {
  utilization {
    media_type                = "call"
    maximum_capacity          = var.call_capacity
    interruptible_media_types = []
  }
}
