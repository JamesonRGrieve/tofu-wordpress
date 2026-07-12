// SPDX-License-Identifier: AGPL-3.0-or-later

// Package provider implements the wordpress OpenTofu/Terraform provider — a
// native manager for WordPress installed state (WP-CLI core/plugin/theme),
// wp-config.php defines, the system-cron wp-cron entry, and safe on-disk
// content-directory relocation, driven over an SSH + WP-CLI transport. Secrets
// (WP admin password, DB password, AUTH salts) are injected at apply from the
// secret store as ephemeral/write-only values and are NEVER stored in state.
package provider

import (
	"context"
	"time"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*wordpressProvider)(nil)

// defaultDocroot is the conventional WordPress document root; resources that omit
// `path` inherit it.
const defaultDocroot = "/var/www/html"

// New returns the provider factory for a given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &wordpressProvider{version: version} }
}

type wordpressProvider struct {
	version string
}

// providerClient is the shared per-provider state handed to every resource: the
// SSH transport and the default docroot.
type providerClient struct {
	SSH     *wordpress.SSHClient
	Docroot string
}

type providerModel struct {
	Host        types.String `tfsdk:"host"`
	Port        types.Int64  `tfsdk:"port"`
	User        types.String `tfsdk:"user"`
	SSHKeyFile  types.String `tfsdk:"ssh_key_file"`
	SSHKeyPEM   types.String `tfsdk:"ssh_key_pem"`
	SSHPassword types.String `tfsdk:"ssh_password"`
	Docroot     types.String `tfsdk:"docroot"`
	Timeout     types.Int64  `tfsdk:"timeout"`
}

func (p *wordpressProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	// Single-token type name → resources are `wordpress_*` (the source address is
	// jamesonrgrieve/wordpress).
	resp.TypeName = "wordpress"
	resp.Version = p.version
}

func (p *wordpressProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Native provider for WordPress installed state, wp-config.php, system cron, and " +
			"safe content-directory relocation, driven over SSH + WP-CLI. Reads its source of truth from " +
			"netbox-wordpress (WordPressSite / WordPressContentDirectory) at the consumer layer; DB and admin " +
			"secrets are injected at apply from OpenBao and never stored in state.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "WordPress host address (host or host:port), no scheme, reached over SSH.",
			},
			"port": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "SSH port (default: the port in `host`, else 22).",
			},
			"user": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "SSH user (default `root`). WP-CLI runs with `--allow-root`.",
			},
			"ssh_key_file": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to an SSH identity file (`ssh -i`). When empty, ssh_config/agent is used.",
			},
			"ssh_key_pem": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "SSH private-key material (e.g. an OpenBao-signed key). Materialized to a temp " +
					"0600 file per call; available at plan time, unlike a Terraform-written key file. A key always " +
					"wins over `ssh_password` when both are set.",
			},
			"ssh_password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "SSH password (e.g. a per-guest root password from OpenBao) for the password-only " +
					"CT fleet. Fed to ssh via `sshpass -e` (in `$SSHPASS`, never argv); requires `sshpass` on the runner. " +
					"Used only when no key material (`ssh_key_file`/`ssh_key_pem`) is supplied.",
			},
			"docroot": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Default WordPress document root (`wp --path`) inherited by any resource that " +
					"omits `path`. Default `" + defaultDocroot + "`.",
			},
			"timeout": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Per-command SSH timeout in seconds (default 120). Raise it for slow operations " +
					"such as a content-directory rsync of a large uploads tree.",
			},
		},
	}
}

func (p *wordpressProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var timeout time.Duration
	if !cfg.Timeout.IsNull() && !cfg.Timeout.IsUnknown() && cfg.Timeout.ValueInt64() > 0 {
		timeout = time.Duration(cfg.Timeout.ValueInt64()) * time.Second
	}
	ssh := wordpress.NewSSHClient(wordpress.SSHConfig{
		Host:     cfg.Host.ValueString(),
		Port:     int(cfg.Port.ValueInt64()),
		User:     cfg.User.ValueString(),
		KeyFile:  cfg.SSHKeyFile.ValueString(),
		KeyPEM:   cfg.SSHKeyPEM.ValueString(),
		Password: cfg.SSHPassword.ValueString(),
		Timeout:  timeout,
	})
	docroot := defaultDocroot
	if !cfg.Docroot.IsNull() && cfg.Docroot.ValueString() != "" {
		docroot = cfg.Docroot.ValueString()
	}
	client := &providerClient{SSH: ssh, Docroot: docroot}
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *wordpressProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCoreResource,
		NewConfigResource,
		NewPluginResource,
		NewThemeResource,
		NewOptionResource,
		NewMuPluginResource,
		NewCronResource,
		NewContentDirResource,
	}
}

func (p *wordpressProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
