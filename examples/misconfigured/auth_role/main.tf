// Duplicate role name — fakegenesys returns 409 on the second create.
resource "genesyscloud_auth_role" "first" {
  name        = "duplicate-role-misconfigured"
  permissions = ["routing:queue:view"]
}

resource "genesyscloud_auth_role" "second" {
  name        = "duplicate-role-misconfigured"
  permissions = ["routing:queue:view"]
  depends_on  = [genesyscloud_auth_role.first]
}
