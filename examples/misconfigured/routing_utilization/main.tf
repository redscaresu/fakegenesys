// utilization is a singleton — a second TF resource would also be a
// singleton PUT, so misconfiguration here is "invalid media type".
// fakegenesys keeps Reverse-Fidelity (doesn't validate media type
// strings), so this dir is a placeholder; the provider's schema
// validation rejects unknown media types before reaching fakegenesys.
resource "genesyscloud_routing_utilization" "bad" {
  utilization {
    media_type        = "bogusMediaType"
    maximum_capacity  = 1
    interruptible_media_types = []
  }
}
