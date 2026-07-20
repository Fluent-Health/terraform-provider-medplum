package provider

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type botModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Code           types.String `tfsdk:"code"`
	SourcePath     types.String `tfsdk:"source_path"`
	SourceHash     types.String `tfsdk:"source_hash"`
	RuntimeVersion types.String `tfsdk:"runtime_version"`
	Timeout        types.Int64  `tfsdk:"timeout"`
	RunAsUser      types.Bool   `tfsdk:"run_as_user"`
	AccessPolicy   types.String `tfsdk:"access_policy"`
	ProjectID      types.String `tfsdk:"project_id"`
	MembershipID   types.String `tfsdk:"membership_id"`
}

// resolveCode returns the bundled bot code from `code` or `source_path`.
// ok=false with a nil error means the value is not yet known (unknown in plan).
func (m botModel) resolveCode() (code string, ok bool, err error) {
	switch {
	case !m.Code.IsNull() && !m.Code.IsUnknown():
		return m.Code.ValueString(), true, nil
	case m.Code.IsUnknown() || m.SourcePath.IsUnknown():
		return "", false, nil
	case !m.SourcePath.IsNull():
		b, err := os.ReadFile(m.SourcePath.ValueString())
		if err != nil {
			return "", false, fmt.Errorf("reading source_path: %w", err)
		}
		return string(b), true, nil
	}
	return "", false, fmt.Errorf("one of code or source_path must be set")
}

func sourceHashOf(code string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(code)))
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// binaryIDFromURL extracts the Binary resource id from Bot.executableCode.url.
// Medplum rewrites attachment URLs on read, so the value can be a plain
// reference ("Binary/{id}"), a FHIR API URL (".../fhir/R4/Binary/{id}"
// optionally followed by "/_history/{vid}"), or a presigned storage URL whose
// path ends with "/{id}/{versionId}". Returns "" when the id is unrecoverable.
func binaryIDFromURL(raw string) string {
	if rest, found := strings.CutPrefix(raw, "Binary/"); found {
		return strings.SplitN(rest, "/", 2)[0]
	}
	if i := strings.Index(raw, "/Binary/"); i >= 0 {
		rest := raw[i+len("/Binary/"):]
		return strings.SplitN(rest, "/", 2)[0]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) >= 2 && uuidRe.MatchString(segs[len(segs)-1]) && uuidRe.MatchString(segs[len(segs)-2]) {
		return segs[len(segs)-2]
	}
	return ""
}

// adminCreateBody builds the plain-JSON BotInitParameters payload for
// POST /admin/projects/{id}/bot. timeout and runAsUser are not part of that
// contract — they are applied with a follow-up PUT (see updateBotFields).
func (m botModel) adminCreateBody() ([]byte, error) {
	doc := map[string]any{
		"name":           m.Name.ValueString(),
		"runtimeVersion": m.RuntimeVersion.ValueString(),
	}
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	}
	if v := strOrEmpty(m.AccessPolicy); v != "" {
		doc["accessPolicy"] = refObj(v)
	}
	return json.Marshal(doc)
}

// applyBotFields writes the model's Bot fields onto doc (the server's current
// Bot JSON), preserving fields the provider does not model (sourceCode,
// executableCode, meta, ...).
func (m botModel) applyBotFields(doc map[string]any) {
	doc["name"] = m.Name.ValueString()
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	} else {
		delete(doc, "description")
	}
	doc["runtimeVersion"] = m.RuntimeVersion.ValueString()
	if m.Timeout.IsNull() || m.Timeout.IsUnknown() {
		delete(doc, "timeout")
	} else {
		doc["timeout"] = m.Timeout.ValueInt64()
	}
	if m.RunAsUser.IsNull() || m.RunAsUser.IsUnknown() {
		delete(doc, "runAsUser")
	} else {
		doc["runAsUser"] = m.RunAsUser.ValueBool()
	}
}

type botDoc struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	RuntimeVersion string `json:"runtimeVersion"`
	Timeout        *int64 `json:"timeout"`
	RunAsUser      *bool  `json:"runAsUser"`
	Meta           struct {
		Project string `json:"project"`
	} `json:"meta"`
	ExecutableCode struct {
		URL string `json:"url"`
	} `json:"executableCode"`
}

// fromDoc maps server Bot fields into the model. Code, SourcePath, SourceHash,
// AccessPolicy and MembershipID are managed by the callers.
func (m *botModel) fromDoc(doc botDoc) {
	m.ID = types.StringValue(doc.ID)
	m.Name = types.StringValue(doc.Name)
	m.Description = optString(doc.Description)
	m.RuntimeVersion = optString(doc.RuntimeVersion)
	if doc.Timeout != nil {
		m.Timeout = types.Int64Value(*doc.Timeout)
	} else {
		m.Timeout = types.Int64Null()
	}
	if doc.RunAsUser != nil {
		m.RunAsUser = types.BoolValue(*doc.RunAsUser)
	} else {
		m.RunAsUser = types.BoolNull()
	}
	m.ProjectID = types.StringValue(doc.Meta.Project)
}
