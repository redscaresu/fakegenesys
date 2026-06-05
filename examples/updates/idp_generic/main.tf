variable "issuer_uri" {
  type = string
}

resource "genesyscloud_idp_generic" "idp" {
  name        = "Okta"
  certificate = "MIIDpzCCAo+gAwIBAgIEAKKjyDANBgkqhkiG9w0BAQsFADBkMSY..."
  issuer_uri  = var.issuer_uri
  target_uri  = "${var.issuer_uri}/sso"
}
