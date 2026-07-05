// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import "github.com/hashicorp/terraform-plugin-framework/resource"

// NewThemeResource constructs the wordpress_theme resource — a componentResource
// bound to the WP-CLI `theme` subcommand.
func NewThemeResource() resource.Resource { return &componentResource{kind: "theme"} }
