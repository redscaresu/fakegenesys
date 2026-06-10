variable "yaml_file" {
  type = string
}

resource "genesyscloud_flow" "ivr" {
  filepath = "${path.module}/${var.yaml_file}"
}
