resource "genesyscloud_flow" "main_ivr" {
  filepath          = "${path.module}/main-ivr.yaml"
  file_content_hash = filemd5("${path.module}/main-ivr.yaml")
}
