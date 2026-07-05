// SPDX-License-Identifier: AGPL-3.0-or-later

// Command tofu-wordpress is the OpenTofu/Terraform provider plugin entrypoint
// for managing WordPress installed state (WP-CLI core/plugin/theme), wp-config.php,
// system cron, and safe on-disk content-directory relocation over SSH + WP-CLI.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/jamesonrgrieve/wordpress",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
