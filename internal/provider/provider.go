package provider

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirschema"
)

// providerData is passed to resources via Configure.
type providerData struct {
	Client         *client.Client
	Validator      *fhirschema.Validator
	MedplumVersion string
	// SupportedBotRuntimes gates medplum_bot.runtime_version at plan time.
	// Never empty; defaults to ["vmcontext"].
	SupportedBotRuntimes []string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &medplumProvider{version: version} }
}

type medplumProvider struct {
	version string
}

type medplumProviderModel struct {
	BaseURL              types.String `tfsdk:"base_url"`
	FHIRPath             types.String `tfsdk:"fhir_path"`
	TokenURL             types.String `tfsdk:"token_url"`
	ClientID             types.String `tfsdk:"client_id"`
	ClientSecret         types.String `tfsdk:"client_secret"`
	AccessToken          types.String `tfsdk:"access_token"`
	Email                types.String `tfsdk:"email"`
	Password             types.String `tfsdk:"password"`
	MedplumVersion       types.String `tfsdk:"medplum_version"`
	SupportedBotRuntimes types.List   `tfsdk:"supported_bot_runtimes"`
}

func (p *medplumProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "medplum"
	resp.Version = p.version
}

func (p *medplumProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage Medplum FHIR resources and project configuration.",
		Attributes: map[string]schema.Attribute{
			"base_url":        schema.StringAttribute{Optional: true, MarkdownDescription: "Medplum (or gateway) base URL. Env: MEDPLUM_BASE_URL."},
			"fhir_path":       schema.StringAttribute{Optional: true, MarkdownDescription: "FHIR base path. Default /fhir/R4. Env: MEDPLUM_FHIR_PATH."},
			"token_url":       schema.StringAttribute{Optional: true, MarkdownDescription: "OAuth token endpoint. Default base_url + /oauth2/token. Env: MEDPLUM_TOKEN_URL."},
			"client_id":       schema.StringAttribute{Optional: true, MarkdownDescription: "OAuth client id. Env: MEDPLUM_CLIENT_ID."},
			"client_secret":   schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "OAuth client secret. Env: MEDPLUM_CLIENT_SECRET."},
			"access_token":    schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Pre-obtained bearer token. Env: MEDPLUM_ACCESS_TOKEN."},
			"email":           schema.StringAttribute{Optional: true, MarkdownDescription: "Super-admin email. Env: MEDPLUM_EMAIL."},
			"password":        schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Super-admin password. Env: MEDPLUM_PASSWORD."},
			"medplum_version": schema.StringAttribute{Optional: true, MarkdownDescription: "Medplum server version used to select the profile support matrix. Default 5.0.10. Env: MEDPLUM_VERSION."},
			"supported_bot_runtimes": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Bot runtimes this environment can execute; medplum_bot.runtime_version outside this set fails at plan time. Subset of vmcontext, fission, awslambda. Default [\"vmcontext\"].",
			},
		},
	}
}

func firstNonEmpty(configured types.String, envKey string) string {
	if !configured.IsNull() && configured.ValueString() != "" {
		return configured.ValueString()
	}
	return os.Getenv(envKey)
}

func (p *medplumProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m medplumProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := client.Config{
		BaseURL:      firstNonEmpty(m.BaseURL, "MEDPLUM_BASE_URL"),
		FHIRPath:     firstNonEmpty(m.FHIRPath, "MEDPLUM_FHIR_PATH"),
		TokenURL:     firstNonEmpty(m.TokenURL, "MEDPLUM_TOKEN_URL"),
		ClientID:     firstNonEmpty(m.ClientID, "MEDPLUM_CLIENT_ID"),
		ClientSecret: firstNonEmpty(m.ClientSecret, "MEDPLUM_CLIENT_SECRET"),
		AccessToken:  firstNonEmpty(m.AccessToken, "MEDPLUM_ACCESS_TOKEN"),
		Email:        firstNonEmpty(m.Email, "MEDPLUM_EMAIL"),
		Password:     firstNonEmpty(m.Password, "MEDPLUM_PASSWORD"),
	}

	c, err := client.New(ctx, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Medplum provider configuration", err.Error())
		return
	}
	v, err := fhirschema.New()
	if err != nil {
		resp.Diagnostics.AddError("Failed to load FHIR schema", err.Error())
		return
	}

	medplumVersion := firstNonEmpty(m.MedplumVersion, "MEDPLUM_VERSION")
	if medplumVersion == "" {
		medplumVersion = "5.0.10"
	}

	supportedRuntimes := listToStrings(m.SupportedBotRuntimes)
	if len(supportedRuntimes) == 0 {
		supportedRuntimes = []string{"vmcontext"}
	}
	if err := validateBotRuntimes(supportedRuntimes); err != nil {
		resp.Diagnostics.AddError("Invalid supported_bot_runtimes", err.Error())
		return
	}

	data := &providerData{Client: c, Validator: v, MedplumVersion: medplumVersion, SupportedBotRuntimes: supportedRuntimes}
	resp.ResourceData = data
	resp.DataSourceData = data
}

// botRuntimes are the runtime_version values Medplum dispatches on
// (bots/execute.ts). The provider default and the only one most self-hosted
// environments support out of the box is vmcontext.
var botRuntimes = []string{"vmcontext", "fission", "awslambda"}

func validateBotRuntimes(supported []string) error {
	for _, rt := range supported {
		if !slices.Contains(botRuntimes, rt) {
			return fmt.Errorf("unknown bot runtime %q: must be a subset of %s", rt, strings.Join(botRuntimes, ", "))
		}
	}
	return nil
}

func (p *medplumProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewFHIRResource,
		NewAccessPolicyResource,
		NewClientApplicationResource,
		NewProjectMembershipResource,
		NewUserResource,
		NewProjectResource,
		NewFHIRProfileResource,
		NewFHIRDataMigrationResource,
	}
}

func (p *medplumProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewFHIRSearchDataSource,
	}
}
