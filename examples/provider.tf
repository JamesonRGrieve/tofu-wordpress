# SPDX-License-Identifier: AGPL-3.0-or-later
# Provider configuration. Credentials are injected at apply from OpenBao via
# TF_VAR_* / ephemeral resources — never hard-coded here.

terraform {
  required_providers {
    wordpress = {
      source  = "jamesonrgrieve/wordpress"
      version = "~> 0.1"
    }
  }
}

provider "wordpress" {
  host    = "wp-301.example.internal" # WordPress host, reached over SSH
  user    = "root"                    # WP-CLI runs with --allow-root
  docroot = "/var/www/html"           # default document root for all resources

  # SSH auth is key/cert only. Prefer an OpenBao-signed key materialized at apply:
  ssh_key_pem = var.ssh_signed_key # sensitive; supplied via TF_VAR_ssh_signed_key
  # ssh_key_file = "/home/runner/.ssh/id_wp"   # or an on-disk identity
}

variable "ssh_signed_key" {
  description = "OpenBao-signed SSH private key (PEM). Injected at apply, never committed."
  type        = string
  sensitive   = true
}
