// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"sort"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// wordpress_config manages wp-config.php defines via `wp config set`/`get`. Only
// the constants declared in `constants` (plus the DB/table attributes) are
// managed — manage-declared-only diff, so unmanaged defines are never clobbered.
// db_password and the AUTH salts are WRITE-ONLY: read from config at apply
// (inject from OpenBao) and never stored in state.
var (
	_ resource.Resource                = (*configResource)(nil)
	_ resource.ResourceWithConfigure   = (*configResource)(nil)
	_ resource.ResourceWithImportState = (*configResource)(nil)
)

// NewConfigResource constructs the wordpress_config resource.
func NewConfigResource() resource.Resource { return &configResource{} }

type configResource struct {
	client *providerClient
}

type configModel struct {
	ID          types.String `tfsdk:"id"`
	Path        types.String `tfsdk:"path"`
	Constants   types.Map    `tfsdk:"constants"`
	TablePrefix types.String `tfsdk:"table_prefix"`
	DBName      types.String `tfsdk:"db_name"`
	DBUser      types.String `tfsdk:"db_user"`
	DBHost      types.String `tfsdk:"db_host"`
	DBPassword  types.String `tfsdk:"db_password"`
	Salts       types.Map    `tfsdk:"salts"`
}

func (r *configResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_config"
}

func (r *configResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "wp-config.php defines managed via WP-CLI, manage-declared-only. Common constants: " +
			"WP_DEBUG, WP_MEMORY_LIMIT, DISABLE_WP_CRON (default true in production), DISALLOW_FILE_EDIT, " +
			"WP_AUTO_UPDATE_CORE, FORCE_SSL_ADMIN, WP_REDIS_* (object cache).",
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
			"constants": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "wp-config.php constants as name→value. Boolean/int constants (WP_DEBUG, " +
					"DISABLE_WP_CRON, WP_POST_REVISIONS, …) are written unquoted (`--raw`); everything else as a string.",
			},
			"table_prefix": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The `$table_prefix` variable (default `wp_`).",
			},
			"db_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "DB_NAME constant.",
			},
			"db_user": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "DB_USER constant.",
			},
			"db_host": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "DB_HOST constant.",
			},
			"db_password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				WriteOnly:           true,
				MarkdownDescription: "DB_PASSWORD constant. WRITE-ONLY: injected from OpenBao at apply, never in state.",
			},
			"salts": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				WriteOnly:   true,
				MarkdownDescription: "AUTH_KEY/SECURE_AUTH_KEY/…/NONCE_SALT as name→value. WRITE-ONLY: injected from " +
					"OpenBao at apply, never stored in state.",
			},
		},
	}
}

func (r *configResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *configResource) docroot() string {
	if r.client == nil {
		return defaultDocroot
	}
	return r.client.Docroot
}

func (r *configResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, &req.Plan, &req.Config, &resp.State, &resp.Diagnostics)
}

func (r *configResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, &req.Plan, &req.Config, &resp.State, &resp.Diagnostics)
}

// write applies the declared config (Create/Update share it). Write-only values
// (db_password, salts) are read from config, not plan.
func (r *configResource) write(ctx context.Context, plan planGetter, config attrGetter, state stateSetter, diags *diag.Diagnostics) {
	var m configModel
	diags.Append(plan.Get(ctx, &m)...)
	if diags.HasError() {
		return
	}
	p := resolvePath(m.Path, r.docroot())
	if r.client != nil && r.client.SSH != nil {
		wp := r.client.wp(p)
		for name, val := range mapValues(ctx, m.Constants, diags) {
			if err := wp.ConfigSet(name, val, wordpress.IsRawConstant(name)); err != nil {
				diags.AddError("wp config set "+name+" failed", err.Error())
				return
			}
		}
		if !m.DBName.IsNull() {
			if err := wp.ConfigSet("DB_NAME", m.DBName.ValueString(), false); err != nil {
				diags.AddError("wp config set DB_NAME failed", err.Error())
				return
			}
		}
		if !m.DBUser.IsNull() {
			if err := wp.ConfigSet("DB_USER", m.DBUser.ValueString(), false); err != nil {
				diags.AddError("wp config set DB_USER failed", err.Error())
				return
			}
		}
		if !m.DBHost.IsNull() {
			if err := wp.ConfigSet("DB_HOST", m.DBHost.ValueString(), false); err != nil {
				diags.AddError("wp config set DB_HOST failed", err.Error())
				return
			}
		}
		if !m.TablePrefix.IsNull() {
			if _, err := wp.Command("config", "set", "table_prefix", m.TablePrefix.ValueString(), "--type=variable"); err != nil {
				diags.AddError("wp config set table_prefix failed", err.Error())
				return
			}
		}
		// Write-only secrets from config.
		var pw types.String
		diags.Append(config.GetAttribute(ctx, path.Root("db_password"), &pw)...)
		if !pw.IsNull() && pw.ValueString() != "" {
			if err := wp.ConfigSet("DB_PASSWORD", pw.ValueString(), false); err != nil {
				diags.AddError("wp config set DB_PASSWORD failed", err.Error())
				return
			}
		}
		var salts types.Map
		diags.Append(config.GetAttribute(ctx, path.Root("salts"), &salts)...)
		for name, val := range mapValues(ctx, salts, diags) {
			if err := wp.ConfigSet(name, val, false); err != nil {
				diags.AddError("wp config set salt "+name+" failed", err.Error())
				return
			}
		}
	}
	m.Path = types.StringValue(p)
	m.ID = types.StringValue(p)
	diags.Append(state.Set(ctx, &m)...)
}

func (r *configResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m configModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || r.client.SSH == nil {
		return
	}
	wp := r.client.wp(resolvePath(m.Path, r.docroot()))

	// Subset refresh: only declared constants are reconciled; equal values keep
	// their declared form (0-diff), drift surfaces the device value.
	declared := mapValues(ctx, m.Constants, &resp.Diagnostics)
	if len(declared) > 0 {
		refreshed := map[string]string{}
		for _, name := range sortedKeys(declared) {
			live, err := wp.ConfigGet(name)
			if err != nil {
				// Constant absent on the device → surface as empty (drift).
				refreshed[name] = ""
				continue
			}
			refreshed[name] = wordpress.ReconcileConstant(name, declared[name], live)
		}
		mv, d := types.MapValueFrom(ctx, types.StringType, refreshed)
		resp.Diagnostics.Append(d...)
		m.Constants = mv
	}
	if !m.DBName.IsNull() {
		if v, err := wp.ConfigGet("DB_NAME"); err == nil {
			m.DBName = types.StringValue(v)
		}
	}
	if !m.DBUser.IsNull() {
		if v, err := wp.ConfigGet("DB_USER"); err == nil {
			m.DBUser = types.StringValue(v)
		}
	}
	if !m.DBHost.IsNull() {
		if v, err := wp.ConfigGet("DB_HOST"); err == nil {
			m.DBHost = types.StringValue(v)
		}
	}
	if !m.TablePrefix.IsNull() {
		if v, err := wp.Command("config", "get", "table_prefix", "--type=variable"); err == nil {
			m.TablePrefix = types.StringValue(v)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Delete is a no-op: wp-config defines persist; the resource stops managing them.
func (r *configResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *configResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// mapValues extracts a Go map from a types.Map (nil-safe: null/unknown → empty).
func mapValues(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out
	}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}

// sortedKeys returns the map keys in deterministic order (stable applies/tests).
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
