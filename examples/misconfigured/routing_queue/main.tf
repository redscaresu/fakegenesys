// Missing required `name` → 400.
resource "genesyscloud_routing_queue" "no_name" {
  description = "queue without a name"
}
