// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Option value formats. `string` is a scalar; `json` manages a serialized
// structured option (a plugin settings array, e.g. AI Engine / OIDC / WP Mail
// SMTP) via `wp option get/update --format=json`. For a json option the value
// must be canonical JSON — emit it with Terraform's `jsonencode()` (sorted keys,
// compact), which matches the canonicalized device read-back so the plan is 0-diff.
const (
	optionFormatString = "string"
	optionFormatJSON   = "json"
)

// wordpress_option manages a single WordPress option (a wp_options row) via
// `wp option get/update/delete`. It is the generic mechanism for the handful of
// infra-load-bearing plugin OPTIONS that are not install-state — notably the
// wps-hide-login slug (`whl_page`): the netbox-wordpress SoT field
// WordPressSite.login_slug is mapped by the consumer layer to a
// wordpress_option{name = "whl_page", value = login_slug} here, so the provider
// (and the login health check) reads one source of truth. Manage-declared-only:
// only the named option is read back and reconciled; nothing else is touched.
var (
	_ resource.Resource                = (*optionResource)(nil)
	_ resource.ResourceWithConfigure   = (*optionResource)(nil)
	_ resource.ResourceWithImportState = (*optionResource)(nil)
)

// NewOptionResource constructs the wordpress_option resource.
func NewOptionResource() resource.Resource { return &optionResource{} }

type optionResource struct {
	client *providerClient
}

type optionModel struct {
	ID     types.String `tfsdk:"id"`
	Path   types.String `tfsdk:"path"`
	Name   types.String `tfsdk:"name"`
	Value  types.String `tfsdk:"value"`
	Format types.String `tfsdk:"format"`
}

func (r *optionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_option"
}

func (r *optionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A single WordPress option (`wp_options` row) managed via `wp option " +
			"get/update/delete`. Use for infra-relevant plugin options such as the wps-hide-login slug " +
			"(`whl_page`). Imports to 0-diff by option name.",
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
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Option name (e.g. `whl_page` for the wps-hide-login slug).",
			},
			"value": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Option value. For `format = \"json\"` supply canonical JSON via " +
					"`jsonencode(...)` (sorted keys, compact) so it matches the canonicalized device read-back.",
			},
			"format": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(optionFormatString),
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "`string` (scalar, default) or `json` (a serialized structured option — a " +
					"plugin settings array — via `wp option … --format=json`).",
			},
		},
	}
}

func (r *optionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *optionResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

func (r *optionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m optionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &m, &resp.State, &resp.Diagnostics)
}

func (r *optionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m optionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &m, &resp.State, &resp.Diagnostics)
}

// write updates the option and records id/path so a re-apply is a no-op.
func (r *optionResource) write(ctx context.Context, m *optionModel, state stateSetter, diags *diag.Diagnostics) {
	format := m.Format.ValueString()
	if !validOptionFormat(format) {
		diags.AddError("invalid option format",
			fmt.Sprintf("format must be %q or %q; got %q", optionFormatString, optionFormatJSON, format))
		return
	}
	if format == optionFormatJSON {
		if _, err := canonicalizeJSON(m.Value.ValueString()); err != nil {
			diags.AddError("option value is not valid JSON",
				"format is \"json\" but value does not parse — supply it via jsonencode(...): "+err.Error())
			return
		}
	}
	p := resolvePath(m.Path, r.docroot())
	if r.client != nil && r.client.SSH != nil {
		wp := r.client.wp(p)
		args := optionUpdateArgs(m.Name.ValueString(), m.Value.ValueString(), format)
		if _, err := wp.Command(args...); err != nil {
			diags.AddError("wp option update failed", err.Error())
			return
		}
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(optionID(p, m.Name.ValueString()))
	diags.Append(state.Set(ctx, m)...)
}

func (r *optionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m optionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	wp := r.client.wp(p)
	var v string
	var err error
	if m.Format.ValueString() == optionFormatJSON {
		v, err = wp.Command("option", "get", m.Name.ValueString(), "--format=json")
	} else {
		v, err = wp.OptionGet(m.Name.ValueString())
	}
	if err != nil {
		// Option gone → drop from state so it is recreated.
		resp.State.RemoveResource(ctx)
		return
	}
	if m.Format.ValueString() == optionFormatJSON {
		// Canonicalize WP's (insertion-ordered) JSON to sorted-key compact form so
		// it matches a jsonencode()-produced config value → 0-diff.
		if c, cerr := canonicalizeJSON(v); cerr == nil {
			v = c
		}
	}
	m.Value = types.StringValue(v)
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(optionID(p, m.Name.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *optionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m optionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	wp := r.client.wp(resolvePath(m.Path, r.docroot()))
	if _, err := wp.Command("option", "delete", m.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("wp option delete failed", err.Error())
	}
}

func (r *optionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	p, name := parseOptionImportID(req.ID)
	if p != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), p)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ── pure helpers (unit-tested) ───────────────────────────────────────────────

// validOptionFormat reports whether s is a supported option value format.
func validOptionFormat(s string) bool {
	return s == optionFormatString || s == optionFormatJSON
}

// optionUpdateArgs builds the `wp option update` args, adding `--format=json`
// for a structured option. Pure — unit-tested.
func optionUpdateArgs(name, value, format string) []string {
	args := []string{"option", "update", name, value}
	if format == optionFormatJSON {
		args = append(args, "--format=json")
	}
	return args
}

// canonicalizeJSON parses and re-marshals a JSON string to Go's canonical form
// (sorted object keys, compact), matching Terraform's jsonencode() so a config
// value and the device read-back compare equal. Pure — unit-tested.
func canonicalizeJSON(s string) (string, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// optionID is the resource id: "<path>#<name>" (option names contain no '#',
// paths no '#', so the split is unambiguous).
func optionID(p, name string) string {
	return strings.TrimRight(p, "/") + "#" + name
}

// parseOptionImportID splits a "<path>#<name>" import id. A bare name (no '#')
// leaves path empty so the provider docroot is used.
func parseOptionImportID(id string) (pathPart, name string) {
	if i := strings.LastIndex(id, "#"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}
