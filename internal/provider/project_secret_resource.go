package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewProjectSecretResource() resource.Resource { return &projectSecretResource{} }

type projectSecretResource struct{ data *providerData }

type projectSecretModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ValueString types.String `tfsdk:"value_string"`
	ProjectID   types.String `tfsdk:"project_id"`
}

func (r *projectSecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_secret"
}

func (r *projectSecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A single named entry in the session project's `Project.secret[]` settings. " +
			"Bots receive all project secrets at execution time as `event.secrets`. " +
			"Every entry is an independent Terraform resource, so unmanaged sibling entries are preserved; " +
			"writes to the shared Project resource are guarded by optimistic concurrency (`If-Match` on the project version) " +
			"with bounded retries, making parallel applies of many secrets safe.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Secret name, unique within the project (the key bots see in `event.secrets`). Changing it replaces the resource.",
			},
			"value_string": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "The secret's string value (`ProjectSetting.valueString`).",
			},
			"project_id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Project the secret lives in. Always the provider session's project.",
			},
		},
	}
}

func (r *projectSecretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

// projectSecrets returns Project.secret as a slice of entry maps.
func projectSecrets(doc map[string]any) []map[string]any {
	arr, _ := doc["secret"].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// findProjectSecret returns the Project.secret entry with the given name, or nil.
func findProjectSecret(doc map[string]any, name string) map[string]any {
	for _, e := range projectSecrets(doc) {
		if e["name"] == name {
			return e
		}
	}
	return nil
}

// upsertProjectSecret sets the named entry to a fresh {name, valueString}
// (replacing any other value[x] choice), appending when absent. Sibling
// entries are untouched.
func upsertProjectSecret(doc map[string]any, name, value string) {
	entry := map[string]any{"name": name, "valueString": value}
	arr, _ := doc["secret"].([]any)
	for i, e := range arr {
		if m, ok := e.(map[string]any); ok && m["name"] == name {
			arr[i] = entry
			return
		}
	}
	doc["secret"] = append(arr, any(entry))
}

// removeProjectSecret deletes just the named entry, preserving all others, and
// reports whether it was present. An emptied array is dropped entirely (FHIR
// forbids empty arrays; Medplum strips them on write anyway).
func removeProjectSecret(doc map[string]any, name string) bool {
	arr, _ := doc["secret"].([]any)
	kept := make([]any, 0, len(arr))
	found := false
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok && m["name"] == name {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return false
	}
	if len(kept) == 0 {
		delete(doc, "secret")
	} else {
		doc["secret"] = kept
	}
	return true
}

// projectSecretMaxAttempts bounds mutateProject's optimistic-concurrency loop.
const projectSecretMaxAttempts = 5

// mutateProject GETs the project, applies mutate to its JSON doc, and PUTs it
// back guarded by If-Match on the GET's meta.versionId. Multiple
// medplum_project_secret resources apply in parallel and race on the single
// Project resource, so a version conflict (HTTP 412/409) re-GETs and re-runs
// mutate on the fresh doc, with small jitter, bounded at
// projectSecretMaxAttempts. mutate returning an error aborts immediately (no
// retry); returning write=false skips the PUT (nothing to change).
func (r *projectSecretResource) mutateProject(ctx context.Context, projectID string, mutate func(doc map[string]any) (write bool, err error)) error {
	var lastErr error
	for attempt := 0; attempt < projectSecretMaxAttempts; attempt++ {
		out, err := r.data.Client.FHIRRead(ctx, "Project", projectID)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := json.Unmarshal(out, &doc); err != nil {
			return err
		}
		meta, _ := doc["meta"].(map[string]any)
		versionID, _ := meta["versionId"].(string)
		if versionID == "" {
			return fmt.Errorf("Project/%s carries no meta.versionId; cannot write safely", projectID)
		}
		write, err := mutate(doc)
		if err != nil {
			return err
		}
		if !write {
			return nil
		}
		body, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		_, err = r.data.Client.FHIRUpdateIfMatch(ctx, "Project", projectID, versionID, body)
		if err == nil {
			return nil
		}
		if !client.IsConflict(err) {
			return err
		}
		lastErr = err
		// Jittered backoff so parallel writers don't re-collide in lockstep.
		delay := time.Duration(attempt+1)*100*time.Millisecond + time.Duration(rand.IntN(150))*time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("updating Project/%s: version conflict persisted after %d attempts: %w", projectID, projectSecretMaxAttempts, lastErr)
}

func (r *projectSecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	projectID, err := r.data.Client.CurrentProjectID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Project discovery failed", err.Error())
		return
	}
	name := m.Name.ValueString()
	err = r.mutateProject(ctx, projectID, func(doc map[string]any) (bool, error) {
		if findProjectSecret(doc, name) != nil {
			return false, fmt.Errorf("a Project.secret entry named %q already exists; adopt it with `terraform import medplum_project_secret.<addr> %s` instead of creating it", name, name)
		}
		upsertProjectSecret(doc, name, m.ValueString.ValueString())
		return true, nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	m.ID = types.StringValue(name)
	m.ProjectID = types.StringValue(projectID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectSecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	projectID, err := r.data.Client.CurrentProjectID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Project discovery failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "Project", projectID)
	if err != nil {
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	name := m.Name.ValueString()
	entry := findProjectSecret(doc, name)
	if entry == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	m.ID = types.StringValue(name)
	m.ProjectID = types.StringValue(projectID)
	// A missing valueString (e.g. the entry was rewritten out-of-band with a
	// different value[x] choice) surfaces as drift and is repaired on apply.
	if v, ok := entry["valueString"].(string); ok {
		m.ValueString = types.StringValue(v)
	} else {
		m.ValueString = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectSecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	projectID := state.ProjectID.ValueString()
	name := plan.Name.ValueString() // name is RequiresReplace, so == state's
	err := r.mutateProject(ctx, projectID, func(doc map[string]any) (bool, error) {
		upsertProjectSecret(doc, name, plan.ValueString.ValueString())
		return true, nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	plan.ID = state.ID
	plan.ProjectID = state.ProjectID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectSecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.mutateProject(ctx, m.ProjectID.ValueString(), func(doc map[string]any) (bool, error) {
		// Already gone (deleted out-of-band): nothing to write.
		return removeProjectSecret(doc, m.Name.ValueString()), nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

// ImportState accepts the secret's name (the entry key in Project.secret[]).
func (r *projectSecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

var (
	_ resource.Resource                = (*projectSecretResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectSecretResource)(nil)
	_ resource.ResourceWithImportState = (*projectSecretResource)(nil)
)
