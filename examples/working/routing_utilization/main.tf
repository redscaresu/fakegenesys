# Provider replaced the `utilization` blocks (media-type as field) with
# named per-media-type blocks (`call`, `chat`, `email`, etc.). Each
# media type configures its own capacity + interruption rules.
resource "genesyscloud_routing_utilization" "org" {
  call {
    maximum_capacity         = 1
    interruptible_media_types = []
  }
  email {
    maximum_capacity         = 3
    interruptible_media_types = ["call"]
  }
}
