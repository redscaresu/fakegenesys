variable "call_capacity" {
  type = number
}

resource "genesyscloud_routing_utilization" "org" {
  call {
    maximum_capacity         = var.call_capacity
    interruptible_media_types = []
  }
}
