terraform {
  required_providers {
    genesyscloud = {
      source  = "mypurecloud/genesyscloud"
      version = "~> 1.55"
    }
  }
}

# The provider talks to fakegenesys via the GENESYSCLOUD_GATEWAY_*
# env vars (PROTOCOL, HOST, PORT) — the provider does NOT accept HCL
# endpoint overrides. For the smoke harness, those env vars are set
# by examples/provider_smoke_test.go when it spawns the per-test
# fakegenesys instance.
provider "genesyscloud" {
  oauthclient_id     = "fake-client-id"
  oauthclient_secret = "fake-client-secret"
  aws_region         = "us-east-1"
}
