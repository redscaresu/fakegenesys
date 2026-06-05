resource "genesyscloud_auth_role" "queue_supervisor" {
  name        = "queue-supervisor-example"
  description = "Supervisor role for routing queues"
  permissions = [
    "routing:queue:edit",
    "routing:queue:view",
  ]
}
