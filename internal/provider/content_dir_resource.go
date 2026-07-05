// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// wordpress_content_dir — the headline resource: content-directory management
// with SAFE relocation. The move is staged (BuildRelocationPlan), copy-verified,
// config-repointed, health-checked, and rolled back on any failure, with the
// original directory retained until health passes. content_dir is computed to
// the target after a move so re-applying is a zero-diff no-op.
var (
	_ resource.Resource                = (*contentDirResource)(nil)
	_ resource.ResourceWithConfigure   = (*contentDirResource)(nil)
	_ resource.ResourceWithImportState = (*contentDirResource)(nil)
)

const defaultContentDir = "/var/www/html/wp-content"

// NewContentDirResource constructs the wordpress_content_dir resource.
func NewContentDirResource() resource.Resource { return &contentDirResource{} }

type contentDirResource struct {
	client *providerClient
}

type contentDirModel struct {
	ID               types.String `tfsdk:"id"`
	Path             types.String `tfsdk:"path"`
	ContentDir       types.String `tfsdk:"content_dir"`
	TargetContentDir types.String `tfsdk:"target_content_dir"`
	UploadsDir       types.String `tfsdk:"uploads_dir"`
	ContentURL       types.String `tfsdk:"content_url"`
	KeepSymlink      types.Bool   `tfsdk:"keep_symlink"`
}

func (r *contentDirResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_content_dir"
}

func (r *contentDirResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "WordPress content-directory management with safe on-disk relocation. When " +
			"`target_content_dir` differs from the current `content_dir`, the provider quiesces the site " +
			"(maintenance mode), rsync-copies with checksum verification, repoints WP_CONTENT_DIR/URL and the " +
			"uploads path, optionally leaves a compatibility symlink, health-checks the live site, and rolls " +
			"back on any failure — never a naive `mv`, and the original is retained until health passes.",
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
			"content_dir": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(defaultContentDir),
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "The CURRENT absolute wp-content path (default `" + defaultContentDir + "`). " +
					"After a relocation this is computed to `target_content_dir`, so a re-apply is a no-op.",
			},
			"target_content_dir": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The DESIRED absolute wp-content path. Setting it different from `content_dir` triggers the staged move.",
			},
			"uploads_dir": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional absolute uploads path (`wp option update upload_path`).",
			},
			"content_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional WP_CONTENT_URL to set alongside the move.",
			},
			"keep_symlink": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Leave a symlink at the old path pointing to the new (default true). The original bytes are retained (renamed) until health passes.",
			},
		},
	}
}

func (r *contentDirResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *contentDirResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

func (r *contentDirResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m contentDirModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.relocate(ctx, &m, &resp.State, &resp.Diagnostics)
}

func (r *contentDirResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m contentDirModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.relocate(ctx, &m, &resp.State, &resp.Diagnostics)
}

// relocate builds and executes the staged relocation plan, then records the new
// current content dir (= target) so the next plan is a no-op.
func (r *contentDirResource) relocate(ctx context.Context, m *contentDirModel, state stateSetter, diags *diag.Diagnostics) {
	p := resolvePath(m.Path, r.docroot())
	plan := wordpress.BuildRelocationPlan(wordpress.RelocationConfig{
		Path:             p,
		ContentDir:       m.ContentDir.ValueString(),
		TargetContentDir: m.TargetContentDir.ValueString(),
		UploadsDir:       m.UploadsDir.ValueString(),
		ContentURL:       m.ContentURL.ValueString(),
		KeepSymlink:      m.KeepSymlink.ValueBool(),
	})
	if r.client != nil && r.client.SSH != nil {
		if err := wordpress.ExecuteRelocation(r.client.SSH, plan); err != nil {
			diags.AddError("wordpress content-dir relocation failed",
				"the relocation was rolled back (config restored, original directory retained): "+err.Error())
			return
		}
	}
	// The move is done: the new current location is the target.
	m.ContentDir = types.StringValue(m.TargetContentDir.ValueString())
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(p + ":" + m.TargetContentDir.ValueString())
	diags.Append(state.Set(ctx, m)...)
}

func (r *contentDirResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m contentDirModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	// The live current content dir is WP_CONTENT_DIR (falls back to the model's
	// value when the constant is undefined — the WordPress default location).
	wp := r.client.wp(resolvePath(m.Path, r.docroot()))
	if v, err := wp.ConfigGet("WP_CONTENT_DIR"); err == nil && v != "" {
		m.ContentDir = types.StringValue(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Delete is a no-op: the content directory and its config define persist; the
// resource simply stops managing the relocation.
func (r *contentDirResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *contentDirResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import id is the docroot path; content_dir is populated on the following Read.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
