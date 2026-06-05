resource "genesyscloud_idp_generic" "okta" {
  name        = "Okta"
  certificate = "MIIDpzCCAo+gAwIBAgIEAKKjyDANBgkqhkiG9w0BAQsFADBkMSY..."
  issuer_uri  = "https://example.okta.com"
  target_uri  = "https://example.okta.com/sso"
}
