variable "yaml_file" {
  type = string
}

resource "genesyscloud_flow" "ivr" {
  filepath          = "${path.module}/${var.yaml_file}"
  file_content_hash = filemd5("${path.module}/${var.yaml_file}")
}
