// idp_generic is a singleton — declaring it twice in the same module
// fails at provider validation (two singletons).
resource "genesyscloud_idp_generic" "first" {
  name        = "Okta"
  certificate = "..."
  issuer_uri  = "https://idp-1.example.com"
  target_uri  = "https://idp-1.example.com/sso"
}

resource "genesyscloud_idp_generic" "second" {
  name        = "Okta2"
  certificate = "..."
  issuer_uri  = "https://idp-2.example.com"
  target_uri  = "https://idp-2.example.com/sso"
  depends_on  = [genesyscloud_idp_generic.first]
}
