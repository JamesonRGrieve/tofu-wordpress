// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Declared install-state for a plugin/theme, matching netbox-wordpress
// PluginStateChoices. `absent` ensures the component is uninstalled (fleet cruft
// removal, e.g. W3TC/LiteSpeed); the two present states mirror the old bool.
const (
	stateActive          = "active"
	statePresentInactive = "present_inactive"
	stateAbsent          = "absent"
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
	State     types.String `tfsdk:"state"`
	Source    types.String `tfsdk:"source"`
	SourceURL types.String `tfsdk:"source_url"`
}

func (r *componentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.kind
}

func (r *componentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A WordPress " + r.kind + " managed via `wp " + r.kind +
			" install/activate/deactivate/delete/update`. `state=absent` uninstalls it. Imports to 0-diff by slug.",
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
			"state": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(stateActive),
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Declared install-state: `active` (default), `present_inactive`, or " +
					"`absent` (uninstall). For a theme, `present_inactive` means installed but not the current theme.",
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
	r.apply(ctx, &m, &resp.State, &resp.Diagnostics)
}

func (r *componentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m componentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &m, &resp.State, &resp.Diagnostics)
}

// apply converges the component to m.State and records the observed result so a
// re-apply is a no-op. Shared by Create and Update.
func (r *componentResource) apply(ctx context.Context, m *componentModel, state stateSetter, diags *diag.Diagnostics) {
	st := m.State.ValueString()
	if !validComponentState(st) {
		diags.AddError("invalid "+r.kind+" state",
			fmt.Sprintf("state must be one of %s, %s, %s; got %q", stateActive, statePresentInactive, stateAbsent, st))
		return
	}
	p := resolvePath(m.Path, r.docroot())
	if r.client != nil && r.client.SSH != nil {
		if err := r.converge(r.client.wp(p), m); err != nil {
			diags.AddError("wp "+r.kind+" "+st+" failed", err.Error())
			return
		}
		r.refresh(m, p)
	}
	if m.Version.IsNull() || m.Version.IsUnknown() {
		m.Version = types.StringValue("")
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(componentID(p, m.Slug.ValueString()))
	diags.Append(state.Set(ctx, m)...)
}

// converge runs the WP-CLI commands that bring the component to m.State.
func (r *componentResource) converge(wp *wordpress.WPCLI, m *componentModel) error {
	slug := m.Slug.ValueString()
	if m.State.ValueString() == stateAbsent {
		if _, err := wp.Command(r.kind, "is-installed", slug); err != nil {
			return nil // already absent
		}
		_, err := wp.Command(r.kind, "delete", slug)
		return err
	}
	active := m.State.ValueString() == stateActive
	target := componentInstallTarget(m.Source.ValueString(), slug, m.SourceURL.ValueString())
	if _, err := wp.Command(componentInstallArgs(r.kind, target, m.Version.ValueString(), active)...); err != nil {
		return err
	}
	if active {
		_, err := wp.Command(r.kind, "activate", slug)
		return err
	}
	// present_inactive: a plugin is deactivated; a theme has no deactivate verb
	// (it is simply not the active theme), so install-without-activate suffices.
	if r.kind == "plugin" {
		_, err := wp.Command(r.kind, "deactivate", slug)
		return err
	}
	return nil
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
		// Not installed → observed state is absent (drift surfaces if the desired
		// state is a present one; a no-op if the desired state is absent).
		m.State = types.StringValue(stateAbsent)
		m.Version = types.StringValue("")
	} else {
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
	// Only delete if still installed, so removing an already-absent resource
	// (state=absent) from config is not an error.
	if _, err := wp.Command(r.kind, "is-installed", m.Slug.ValueString()); err != nil {
		return
	}
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

// refresh reads the live version/state into the model (best-effort). Called only
// when the component is installed, so the observed state is one of the present
// values.
func (r *componentResource) refresh(m *componentModel, p string) {
	wp := r.client.wp(p)
	slug := m.Slug.ValueString()
	if v, err := wp.Command(r.kind, "get", slug, "--field=version"); err == nil {
		m.Version = types.StringValue(v)
	}
	if s, err := wp.Command(r.kind, "get", slug, "--field=status"); err == nil {
		m.State = types.StringValue(componentStatusState(s))
	}
}

// ── pure helpers (unit-tested) ───────────────────────────────────────────────

// validComponentState reports whether s is one of the three declared states.
func validComponentState(s string) bool {
	return s == stateActive || s == statePresentInactive || s == stateAbsent
}

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

// componentStatusState maps a `wp <kind> get --field=status` value to the
// declared present state: "active"/"active-network"/… ⇒ active, else
// present_inactive. Called only for an installed component.
func componentStatusState(status string) string {
	if strings.HasPrefix(strings.TrimSpace(status), "active") {
		return stateActive
	}
	return statePresentInactive
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
