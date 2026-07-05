// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"fmt"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// planGetter, attrGetter, and stateSetter abstract the request/response plumbing
// so a resource's Create and Update can share one apply routine. tfsdk.Plan,
// tfsdk.Config, and *tfsdk.State satisfy them respectively.
type planGetter interface {
	Get(ctx context.Context, target interface{}) diag.Diagnostics
}

type attrGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target interface{}) diag.Diagnostics
}

type stateSetter interface {
	Set(ctx context.Context, val interface{}) diag.Diagnostics
}

// configureClient extracts the shared *providerClient from a resource Configure
// request, adding a diagnostic on a type mismatch. Returns nil before the
// provider is configured (ProviderData nil), which callers treat as "skip".
func configureClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *providerClient {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*providerClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *providerClient, got %T", req.ProviderData))
		return nil
	}
	return client
}

// wp binds a WP-CLI wrapper to a docroot over the provider's SSH transport.
func (c *providerClient) wp(path string) *wordpress.WPCLI {
	return wordpress.NewWPCLI(c.SSH, path)
}

// resolvePath returns the resource's declared path, or the provider default
// docroot when the attribute is null/empty.
func resolvePath(path types.String, docroot string) string {
	if !path.IsNull() && path.ValueString() != "" {
		return path.ValueString()
	}
	return docroot
}
