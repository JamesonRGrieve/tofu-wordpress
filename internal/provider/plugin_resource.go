// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import "github.com/hashicorp/terraform-plugin-framework/resource"

// NewPluginResource constructs the wordpress_plugin resource — a componentResource
// bound to the WP-CLI `plugin` subcommand.
func NewPluginResource() resource.Resource { return &componentResource{kind: "plugin"} }
