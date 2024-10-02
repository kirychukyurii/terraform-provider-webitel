// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/tls"
	"net/url"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	webitel "github.com/kirychukyurii/webitel-openapi-client-go/client"
)

// Ensure WebitelProvider satisfies various provider interfaces.
var _ provider.Provider = &WebitelProvider{}
var _ provider.ProviderWithFunctions = &WebitelProvider{}

// WebitelProvider defines the provider implementation.
type WebitelProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// WebitelProviderModel describes the provider data model.
type WebitelProviderModel struct {
	Token    types.String `tfsdk:"token"`
	Endpoint types.String `tfsdk:"endpoint"`
	Insecure types.Bool   `tfsdk:"insecure"`
	Retry    types.Object `tfsdk:"retry"`
}

type Retry struct {
	Attempts types.Int64 `tfsdk:"attempts"`
	Delay    types.Int64 `tfsdk:"delay_ms"`
}

func (p *WebitelProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "webitel"
	resp.Version = p.version
}

func (p *WebitelProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Description: "The authentication token used to connect to Webitel. The value can be sourced from " +
					"the `WEBITEL_AUTH_TOKEN` environment variable.",
				Required:  true,
				Sensitive: true,
			},
			"endpoint": schema.StringAttribute{
				Description: "The target Webitel Base API URL in the format `https://[hostname]/api/`. " +
					"The value can be sourced from the `WEBITEL_BASE_URL` environment variable.",
				Required: true,
			},
			"insecure": schema.BoolAttribute{
				Description: "Explicitly allow the provider to perform \"insecure\" SSL requests. If omitted, " +
					"default value is `false`",
				Optional: true,
			},
		},
		Blocks: map[string]schema.Block{
			"retry": schema.SingleNestedBlock{
				Description: "Retry request configuration. By default there are no retries. Configuring this block will result in " +
					"retries if a 420 or 5xx-range status code is received.",
				Attributes: map[string]schema.Attribute{
					"attempts": schema.Int64Attribute{
						Description: "The number of times the request is to be retried. For example, if 2 is specified, " +
							"the request will be tried a maximum of 3 times.",
						Optional: true,
						Validators: []validator.Int64{
							int64validator.AtLeast(0),
						},
					},
					"delay_ms": schema.Int64Attribute{
						Description: "The delay between retry requests in milliseconds.",
						Optional:    true,
						Validators: []validator.Int64{
							int64validator.AtLeast(0),
						},
					},
				},
			},
		},
	}
}

func (p *WebitelProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data WebitelProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var retry Retry
	if !data.Retry.IsNull() && !data.Retry.IsUnknown() {
		resp.Diagnostics.Append(data.Retry.As(ctx, &retry, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.
	host := os.Getenv("WEBITEL_BASE_URL")
	token := os.Getenv("WEBITEL_AUTH_TOKEN")
	if !data.Endpoint.IsNull() {
		host = data.Endpoint.ValueString()
	}

	if !data.Token.IsNull() {
		token = data.Token.ValueString()
	}

	u, err := url.Parse(host)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Webitel API Client",
			"An unexpected error occurred when creating the Webitel API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Webitel Client Error: "+err.Error(),
		)

		return
	}

	cfg := &webitel.TransportConfig{
		// Host is the doman name or IP address of the host that serves the API.
		Host: u.Host,

		// BasePath is the URL prefix for all API paths, relative to the host root.
		BasePath: u.Path,

		// Schemes are the transfer protocols used by the API (http or https).
		Schemes: []string{u.Scheme},

		// APIKey is an optional API key or service account token.
		APIKey: token,

		// TLSConfig provides an optional configuration for a TLS client
		TLSConfig: &tls.Config{
			InsecureSkipVerify: data.Insecure.ValueBool(),
		},

		// NumRetries contains the optional number of attempted retries
		NumRetries: int(retry.Attempts.ValueInt64()),

		// RetryTimeout sets an optional time to wait before retrying a request
		RetryTimeout: time.Duration(retry.Delay.ValueInt64()) * time.Millisecond,

		// RetryStatusCodes contains the optional list of status codes to retry
		// Use "x" as a wildcard for a single digit (default: [429, 5xx])
		RetryStatusCodes: []string{"420", "408", "5xx"},

		// HTTPHeaders contains an optional map of HTTP headers to add to each request
		HTTPHeaders: map[string]string{},
	}

	client := webitel.NewHTTPClientWithConfig(strfmt.Default, cfg)

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *WebitelProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewContactResource,
	}
}

func (p *WebitelProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *WebitelProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{
		NewUniqueContactFunction,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &WebitelProvider{
			version: version,
		}
	}
}
