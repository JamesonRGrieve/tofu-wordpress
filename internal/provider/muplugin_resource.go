// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// wordpress_muplugin manages a must-use plugin file at
// `<docroot>/wp-content/mu-plugins/<name>` by its full content. It is the
// declarative home for the shim mu-plugins the integration playbooks deploy
// (Matomo tracking, ntfy/Matrix notifications, Tika/SearXNG shims, the Frappe
// order-sync bridge). `content` is the whole file body; Read reads it back so an
// out-of-band edit is detected as drift. Delete removes the file.
var (
	_ resource.Resource                = (*muPluginResource)(nil)
	_ resource.ResourceWithConfigure   = (*muPluginResource)(nil)
	_ resource.ResourceWithImportState = (*muPluginResource)(nil)
)

// NewMuPluginResource constructs the wordpress_muplugin resource.
func NewMuPluginResource() resource.Resource { return &muPluginResource{} }

type muPluginResource struct {
	client *providerClient
}

type muPluginModel struct {
	ID      types.String `tfsdk:"id"`
	Path    types.String `tfsdk:"path"`
	Name    types.String `tfsdk:"name"`
	Content types.String `tfsdk:"content"`
}

func (r *muPluginResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_muplugin"
}

func (r *muPluginResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A WordPress must-use plugin file at `wp-content/mu-plugins/<name>`, managed by its " +
			"full content. Read reads the file back, so an out-of-band edit shows as drift. Imports to 0-diff.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"path": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "WordPress document root. Defaults to the provider `docroot`.",
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Mu-plugin filename, e.g. `zz-matomo.php`.",
			},
			"content": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The complete PHP file body written to the mu-plugin.",
			},
		},
	}
}

func (r *muPluginResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *muPluginResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

func (r *muPluginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m muPluginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &m, &resp.State, &resp.Diagnostics)
}

func (r *muPluginResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m muPluginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &m, &resp.State, &resp.Diagnostics)
}

// write creates the mu-plugins dir (if absent) and writes the file body, then
// records id/path so a re-apply is a no-op.
func (r *muPluginResource) write(ctx context.Context, m *muPluginModel, state stateSetter, diags *diag.Diagnostics) {
	p := resolvePath(m.Path, r.docroot())
	file := muPluginFile(p, m.Name.ValueString())
	if r.client != nil && r.client.SSH != nil {
		cmd := "mkdir -p " + shellQuote(muPluginDir(p)) + " && cat > " + shellQuote(file)
		if _, err := r.client.SSH.Run(cmd, []byte(m.Content.ValueString())); err != nil {
			diags.AddError("write wordpress mu-plugin failed", err.Error())
			return
		}
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(file)
	diags.Append(state.Set(ctx, m)...)
}

func (r *muPluginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m muPluginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	file := muPluginFile(p, m.Name.ValueString())
	out, err := r.client.SSH.Run("cat "+shellQuote(file), nil)
	if err != nil {
		// File gone → drop from state so it is recreated.
		resp.State.RemoveResource(ctx)
		return
	}
	m.Content = types.StringValue(string(out))
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(file)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *muPluginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m muPluginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	file := muPluginFile(resolvePath(m.Path, r.docroot()), m.Name.ValueString())
	if _, err := r.client.SSH.Run("rm -f "+shellQuote(file), nil); err != nil {
		resp.Diagnostics.AddError("remove wordpress mu-plugin failed", err.Error())
	}
}

func (r *muPluginResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	p, name := parseMuPluginImportID(req.ID)
	if p != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), p)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), muPluginFile(p, name))...)
}

// ── pure helpers (unit-tested) ───────────────────────────────────────────────

// muPluginDir is the mu-plugins directory for a docroot.
func muPluginDir(docroot string) string {
	return strings.TrimRight(docroot, "/") + "/wp-content/mu-plugins"
}

// muPluginFile is the absolute path of a mu-plugin file.
func muPluginFile(docroot, name string) string {
	return muPluginDir(docroot) + "/" + name
}

// parseMuPluginImportID splits a "<docroot>#<name>" import id. A bare name (no
// '#') leaves path empty so the provider docroot is used.
func parseMuPluginImportID(id string) (pathPart, name string) {
	if i := strings.LastIndex(id, "#"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}
