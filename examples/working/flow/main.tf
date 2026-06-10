resource "genesyscloud_flow" "main_ivr" {
  filepath = "${path.module}/main-ivr.yaml"
}
