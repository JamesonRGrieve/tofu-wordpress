// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// wordpress_core installs and updates the WordPress core at a docroot via
// WP-CLI. read = `wp core version`; create = `wp core download` + `wp core
// install`; update = `wp core update --version=`. Admin credentials for the
// install are WRITE-ONLY (admin_password) or non-secret attributes — the
// password is read from config at apply and never persisted to state.
var (
	_ resource.Resource                = (*coreResource)(nil)
	_ resource.ResourceWithConfigure   = (*coreResource)(nil)
	_ resource.ResourceWithImportState = (*coreResource)(nil)
)

// NewCoreResource constructs the wordpress_core resource.
func NewCoreResource() resource.Resource { return &coreResource{} }

type coreResource struct {
	client *providerClient
}

type coreModel struct {
	ID            types.String `tfsdk:"id"`
	Path          types.String `tfsdk:"path"`
	Version       types.String `tfsdk:"version"`
	Locale        types.String `tfsdk:"locale"`
	URL           types.String `tfsdk:"url"`
	Title         types.String `tfsdk:"title"`
	AdminUser     types.String `tfsdk:"admin_user"`
	AdminEmail    types.String `tfsdk:"admin_email"`
	AdminPassword types.String `tfsdk:"admin_password"`
}

func (r *coreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_core"
}

func (r *coreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "WordPress core install/update via WP-CLI. Imports to 0-diff from an existing install.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"path": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "WordPress document root (`wp --path`). Defaults to the provider `docroot`.",
			},
			"version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Target core version (`wp core update --version=`). Omit to track the installed version.",
			},
			"locale": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Core locale for `wp core download` (e.g. `en_US`).",
			},
			"url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Site URL for `wp core install` (`--url`).",
			},
			"title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Site title for `wp core install` (`--title`).",
			},
			"admin_user": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Admin username for `wp core install` (`--admin_user`). Non-secret.",
			},
			"admin_email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Admin email for `wp core install` (`--admin_email`). Non-secret.",
			},
			"admin_password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				MarkdownDescription: "Admin password for `wp core install` (`--admin_password`). WRITE-ONLY: read from " +
					"config at apply (inject from OpenBao) and NEVER stored in state.",
			},
		},
	}
}

func (r *coreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *coreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m coreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	// admin_password is write-only: read it from config, not plan/state.
	var pw types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("admin_password"), &pw)...)
	if r.client != nil && r.client.SSH != nil {
		wp := r.client.wp(p)
		if !wp.CoreIsInstalled() {
			dl := []string{"core", "download"}
			if v := m.Version.ValueString(); v != "" {
				dl = append(dl, "--version="+v)
			}
			if l := m.Locale.ValueString(); l != "" {
				dl = append(dl, "--locale="+l)
			}
			if _, err := wp.Command(dl...); err != nil {
				resp.Diagnostics.AddError("wp core download failed", err.Error())
				return
			}
			if _, err := wp.Command(coreInstallArgs(m, pw.ValueString())...); err != nil {
				resp.Diagnostics.AddError("wp core install failed", err.Error())
				return
			}
		}
		if v, err := wp.CoreVersion(); err == nil {
			m.Version = types.StringValue(v)
		}
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *coreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m coreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	wp := r.client.wp(resolvePath(m.Path, r.docroot()))
	if !wp.CoreIsInstalled() {
		resp.State.RemoveResource(ctx)
		return
	}
	if v, err := wp.CoreVersion(); err == nil {
		m.Version = types.StringValue(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *coreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m coreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	if r.client != nil && r.client.SSH != nil && m.Version.ValueString() != "" {
		wp := r.client.wp(p)
		if _, err := wp.Command("core", "update", "--version="+m.Version.ValueString()); err != nil {
			resp.Diagnostics.AddError("wp core update failed", err.Error())
			return
		}
		if v, err := wp.CoreVersion(); err == nil {
			m.Version = types.StringValue(v)
		}
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Delete is a no-op: uninstalling WordPress core is destructive and out of scope;
// the resource simply stops managing it.
func (r *coreResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *coreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import id is the docroot path.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *coreResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

// coreInstallArgs builds the `wp core install` arguments from the model and the
// write-only admin password. Pure — unit-tested. An empty optional flag is
// omitted so WP-CLI applies its own default.
func coreInstallArgs(m coreModel, adminPassword string) []string {
	args := []string{"core", "install"}
	appendFlag := func(name, val string) {
		if val != "" {
			args = append(args, "--"+name+"="+val)
		}
	}
	appendFlag("url", m.URL.ValueString())
	appendFlag("title", m.Title.ValueString())
	appendFlag("admin_user", m.AdminUser.ValueString())
	appendFlag("admin_email", m.AdminEmail.ValueString())
	appendFlag("admin_password", adminPassword)
	args = append(args, "--skip-email")
	return args
}
