resource "genesyscloud_routing_utilization" "org" {
  utilization {
    media_type        = "call"
    maximum_capacity  = 1
    interruptible_media_types = []
  }
  utilization {
    media_type        = "email"
    maximum_capacity  = 3
    interruptible_media_types = ["call"]
  }
}
