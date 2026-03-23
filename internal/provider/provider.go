package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = &temporalSchedulesProvider{}

type temporalSchedulesProvider struct {
	version string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &temporalSchedulesProvider{version: version}
	}
}

func (p *temporalSchedulesProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "temporalschedules"
	resp.Version = p.version
}

func (p *temporalSchedulesProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for managing Temporal Schedules. Connection details (address, api_key, namespace) are configured per-resource.",
	}
}

func (p *temporalSchedulesProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *temporalSchedulesProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}

func (p *temporalSchedulesProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
