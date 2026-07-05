// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
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

// wordpress_cron manages the system cron entry that drives wp-cron on a fixed
// cadence (the production model: DISABLE_WP_CRON=true in wp-config.php + a
// /etc/cron.d entry hitting wp-cron.php). read parses the cron file.
var (
	_ resource.Resource                = (*cronResource)(nil)
	_ resource.ResourceWithConfigure   = (*cronResource)(nil)
	_ resource.ResourceWithImportState = (*cronResource)(nil)
)

const (
	defaultCronMinute = "*/5"
	defaultPHPBinary  = "/usr/bin/php"
	defaultCronUser   = "www-data"
)

// NewCronResource constructs the wordpress_cron resource.
func NewCronResource() resource.Resource { return &cronResource{} }

type cronResource struct {
	client *providerClient
}

type cronModel struct {
	ID        types.String `tfsdk:"id"`
	Path      types.String `tfsdk:"path"`
	Minute    types.String `tfsdk:"minute"`
	PHPBinary types.String `tfsdk:"php_binary"`
	User      types.String `tfsdk:"user"`
}

func (r *cronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cron"
}

func (r *cronResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "System cron entry driving wp-cron (pairs with DISABLE_WP_CRON=true). Writes an " +
			"`/etc/cron.d` file that runs wp-cron.php on the given cadence as the web user.",
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
			"minute": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(defaultCronMinute),
				MarkdownDescription: "Cron minute field (default `" + defaultCronMinute + "`).",
			},
			"php_binary": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(defaultPHPBinary),
				MarkdownDescription: "PHP binary that runs wp-cron.php (default `" + defaultPHPBinary + "`).",
			},
			"user": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(defaultCronUser),
				MarkdownDescription: "Cron user (the web user; default `" + defaultCronUser + "`).",
			},
		},
	}
}

func (r *cronResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *cronResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

func (r *cronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m cronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &m, &resp.State, &resp.Diagnostics)
}

func (r *cronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m cronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &m, &resp.State, &resp.Diagnostics)
}

func (r *cronResource) apply(ctx context.Context, m *cronModel, state stateSetter, diags *diag.Diagnostics) {
	p := resolvePath(m.Path, r.docroot())
	file := cronFilePath(p)
	if r.client != nil && r.client.SSH != nil {
		content := wordpress.RenderCronEntry(m.Minute.ValueString(), m.User.ValueString(), m.PHPBinary.ValueString(), p)
		cmd := "cat > " + shellQuote(file)
		if _, err := r.client.SSH.Run(cmd, []byte(content)); err != nil {
			diags.AddError("write wordpress cron file failed", err.Error())
			return
		}
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(file)
	diags.Append(state.Set(ctx, m)...)
}

func (r *cronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m cronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	file := cronFilePath(resolvePath(m.Path, r.docroot()))
	out, err := r.client.SSH.Run("cat "+shellQuote(file)+" 2>/dev/null || true", nil)
	if err != nil {
		resp.Diagnostics.AddError("read wordpress cron file failed", err.Error())
		return
	}
	if minute := wordpress.ParseCronMinute(string(out)); minute != "" {
		m.Minute = types.StringValue(minute)
	} else {
		// File gone → drop from state so it is recreated.
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *cronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m cronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	file := cronFilePath(resolvePath(m.Path, r.docroot()))
	if _, err := r.client.SSH.Run("rm -f "+shellQuote(file), nil); err != nil {
		resp.Diagnostics.AddError("remove wordpress cron file failed", err.Error())
	}
}

func (r *cronResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), cronFilePath(req.ID))...)
}

// cronFilePath is the deterministic /etc/cron.d file for a docroot, so multiple
// WordPress installs on one host never collide. Pure — unit-tested.
func cronFilePath(docroot string) string {
	return "/etc/cron.d/wordpress-cron-" + sanitizeForFilename(docroot)
}

// sanitizeForFilename maps a path to a safe cron.d filename component (cron.d
// filenames must not contain dots or slashes).
func sanitizeForFilename(s string) string {
	s = strings.Trim(s, "/")
	var b strings.Builder
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "root"
	}
	return out
}

// shellQuote single-quotes a value for safe use in a remote POSIX shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
