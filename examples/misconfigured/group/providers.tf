terraform {
  required_providers {
    genesyscloud = {
      source  = "mypurecloud/genesyscloud"
      version = "~> 1.55"
    }
  }
}

# Point the provider at fakegenesys. The genesyscloud provider's
# `genesyscloud_sdk_debug_uri` attribute lets us override the SDK
# gateway hostname; combined with placeholder credentials, this is
# how the smoke harness drives the real provider against the mock.
provider "genesyscloud" {
  oauthclient_id                = "fake-client-id"
  oauthclient_secret            = "fake-client-secret"
  aws_region                    = "us-east-1"
  sdk_debug                     = false
  genesyscloud_alt_gateway_host = "http://localhost:8083"
}
