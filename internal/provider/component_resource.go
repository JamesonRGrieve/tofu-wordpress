// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// componentResource is the shared implementation behind wordpress_plugin and
// wordpress_theme — the two differ only by the WP-CLI subcommand (`plugin` vs
// `theme`), so they share one CRUD body (DRY) parameterized by `kind`.
var (
	_ resource.Resource                = (*componentResource)(nil)
	_ resource.ResourceWithConfigure   = (*componentResource)(nil)
	_ resource.ResourceWithImportState = (*componentResource)(nil)
)

type componentResource struct {
	kind   string // "plugin" | "theme"
	client *providerClient
}

type componentModel struct {
	ID        types.String `tfsdk:"id"`
	Path      types.String `tfsdk:"path"`
	Slug      types.String `tfsdk:"slug"`
	Version   types.String `tfsdk:"version"`
	Active    types.Bool   `tfsdk:"active"`
	Source    types.String `tfsdk:"source"`
	SourceURL types.String `tfsdk:"source_url"`
}

func (r *componentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.kind
}

func (r *componentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A WordPress " + r.kind + " managed via `wp " + r.kind +
			" install/activate/deactivate/delete/update`. Imports to 0-diff by slug.",
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
			"slug": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The " + r.kind + " slug (e.g. `woocommerce`).",
			},
			"version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Pinned version; blank installs/tracks the latest.",
			},
			"active": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Whether the " + r.kind + " is activated (default true).",
			},
			"source": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Install source: `wporg` (default), `url`, or `zip`.",
			},
			"source_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URL or on-host zip path when `source` is `url`/`zip`.",
			},
		},
	}
}

func (r *componentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *componentResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

func (r *componentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m componentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	if r.client != nil && r.client.SSH != nil {
		wp := r.client.wp(p)
		target := componentInstallTarget(m.Source.ValueString(), m.Slug.ValueString(), m.SourceURL.ValueString())
		args := componentInstallArgs(r.kind, target, m.Version.ValueString(), m.Active.ValueBool())
		if _, err := wp.Command(args...); err != nil {
			resp.Diagnostics.AddError("wp "+r.kind+" install failed", err.Error())
			return
		}
		r.refresh(&m, p)
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(componentID(p, m.Slug.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *componentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m componentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	wp := r.client.wp(p)
	if _, err := wp.Command(r.kind, "is-installed", m.Slug.ValueString()); err != nil {
		resp.State.RemoveResource(ctx)
		return
	}
	r.refresh(&m, p)
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(componentID(p, m.Slug.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *componentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m componentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	if r.client != nil && r.client.SSH != nil {
		wp := r.client.wp(p)
		slug := m.Slug.ValueString()
		if v := m.Version.ValueString(); v != "" {
			if _, err := wp.Command(r.kind, "update", slug, "--version="+v); err != nil {
				resp.Diagnostics.AddError("wp "+r.kind+" update failed", err.Error())
				return
			}
		}
		activateCmd := "deactivate"
		if m.Active.ValueBool() {
			activateCmd = "activate"
		}
		if _, err := wp.Command(r.kind, activateCmd, slug); err != nil {
			resp.Diagnostics.AddError("wp "+r.kind+" "+activateCmd+" failed", err.Error())
			return
		}
		r.refresh(&m, p)
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(componentID(p, m.Slug.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *componentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m componentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	wp := r.client.wp(resolvePath(m.Path, r.docroot()))
	if _, err := wp.Command(r.kind, "delete", m.Slug.ValueString()); err != nil {
		resp.Diagnostics.AddError("wp "+r.kind+" delete failed", err.Error())
	}
}

func (r *componentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	p, slug := parseComponentImportID(req.ID)
	if p != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), p)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), slug)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// refresh reads the live version/active state into the model (best-effort).
func (r *componentResource) refresh(m *componentModel, p string) {
	wp := r.client.wp(p)
	slug := m.Slug.ValueString()
	if v, err := wp.Command(r.kind, "get", slug, "--field=version"); err == nil {
		m.Version = types.StringValue(v)
	}
	if s, err := wp.Command(r.kind, "get", slug, "--field=status"); err == nil {
		m.Active = types.BoolValue(componentStatusActive(s))
	}
}

// ── pure helpers (unit-tested) ───────────────────────────────────────────────

// componentInstallTarget selects the `wp <kind> install` argument: the source
// URL/zip path for non-wporg sources, else the slug.
func componentInstallTarget(source, slug, sourceURL string) string {
	if (source == "url" || source == "zip") && sourceURL != "" {
		return sourceURL
	}
	return slug
}

// componentInstallArgs builds the `wp <kind> install` arguments. A pinned
// version and an activation flag are appended when set.
func componentInstallArgs(kind, target, version string, activate bool) []string {
	args := []string{kind, "install", target}
	if version != "" {
		args = append(args, "--version="+version)
	}
	if activate {
		args = append(args, "--activate")
	}
	return args
}

// componentStatusActive maps a `wp <kind> get --field=status` value to a bool.
// Both plugins and themes report "active"/"active-network"/… when enabled.
func componentStatusActive(status string) bool {
	return strings.HasPrefix(strings.TrimSpace(status), "active")
}

// componentID is the resource id: "<path>/<slug>".
func componentID(p, slug string) string {
	return strings.TrimRight(p, "/") + "/" + slug
}

// parseComponentImportID splits a "<path>/<slug>" import id. A bare slug (no
// slash) leaves path empty so the provider docroot is used.
func parseComponentImportID(id string) (pathPart, slug string) {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}
