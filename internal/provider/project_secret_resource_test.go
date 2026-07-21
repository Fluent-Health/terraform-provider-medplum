package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func TestProjectSecret_findUpsertRemove(t *testing.T) {
	doc := map[string]any{
		"resourceType": "Project",
		"secret": []any{
			map[string]any{"name": "a", "valueString": "1"},
			map[string]any{"name": "b", "valueBoolean": true},
		},
	}

	if e := findProjectSecret(doc, "a"); e == nil || e["valueString"] != "1" {
		t.Fatalf("find a: %v", e)
	}
	if e := findProjectSecret(doc, "nope"); e != nil {
		t.Fatalf("find nope: expected nil, got %v", e)
	}

	// Upsert replaces the whole entry, dropping any other value[x] choice.
	upsertProjectSecret(doc, "b", "now-a-string")
	e := findProjectSecret(doc, "b")
	if e == nil || e["valueString"] != "now-a-string" {
		t.Fatalf("upsert existing: %v", e)
	}
	if _, present := e["valueBoolean"]; present {
		t.Fatal("upsert must drop the previous value[x] choice")
	}

	// Upsert appends when absent, preserving the other entries.
	upsertProjectSecret(doc, "c", "3")
	if e := findProjectSecret(doc, "c"); e == nil || e["valueString"] != "3" {
		t.Fatalf("upsert new: %v", e)
	}
	if e := findProjectSecret(doc, "a"); e == nil || e["valueString"] != "1" {
		t.Fatalf("sibling entry lost on upsert: %v", e)
	}

	// Remove deletes just the named entry.
	if !removeProjectSecret(doc, "a") {
		t.Fatal("remove a: expected true")
	}
	if e := findProjectSecret(doc, "a"); e != nil {
		t.Fatalf("a still present after remove: %v", e)
	}
	if findProjectSecret(doc, "b") == nil || findProjectSecret(doc, "c") == nil {
		t.Fatal("siblings lost on remove")
	}
	if removeProjectSecret(doc, "nope") {
		t.Fatal("remove nope: expected false")
	}
}

func TestProjectSecret_upsertIntoProjectWithoutSecrets(t *testing.T) {
	doc := map[string]any{"resourceType": "Project"}
	upsertProjectSecret(doc, "a", "1")
	if e := findProjectSecret(doc, "a"); e == nil || e["valueString"] != "1" {
		t.Fatalf("upsert into empty project: %v", e)
	}
}

func TestProjectSecret_removeLastDropsArray(t *testing.T) {
	doc := map[string]any{
		"resourceType": "Project",
		"secret":       []any{map[string]any{"name": "a", "valueString": "1"}},
	}
	if !removeProjectSecret(doc, "a") {
		t.Fatal("expected removal")
	}
	// FHIR forbids empty arrays (Medplum strips them on write anyway).
	if _, present := doc["secret"]; present {
		t.Fatalf("empty secret array must be dropped: %v", doc["secret"])
	}
}

// newTestSecretResource builds a projectSecretResource whose client talks to
// the given handler, for exercising the read-modify-write loop without a server.
func newTestSecretResource(t *testing.T, h http.HandlerFunc) (*projectSecretResource, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := client.New(context.Background(), client.Config{BaseURL: srv.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return &projectSecretResource{data: &providerData{Client: c}}, srv
}

// TestProjectSecret_mutateProjectRetriesOnConflict simulates the parallel-apply
// race: the first PUT hits a stale If-Match (HTTP 412), and the loop must
// re-GET the bumped version and succeed on the retry.
func TestProjectSecret_mutateProjectRetriesOnConflict(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	var puts atomic.Int64
	r, _ := newTestSecretResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = fmt.Fprintf(w, `{"resourceType":"Project","id":"p-1","meta":{"versionId":"%d"}}`, version.Load())
		case http.MethodPut:
			want := fmt.Sprintf(`W/"%d"`, version.Load())
			if puts.Add(1) == 1 {
				// Simulate a concurrent writer landing between GET and PUT.
				version.Add(1)
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"conflict","diagnostics":"Precondition Failed"}]}`))
				return
			}
			if got := req.Header.Get("If-Match"); got != want {
				t.Errorf("stale If-Match after retry: got %s, want %s", got, want)
			}
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = fmt.Fprintf(w, `{"resourceType":"Project","id":"p-1","meta":{"versionId":"%d"}}`, version.Load()+1)
		default:
			http.Error(w, "bad method", http.StatusBadRequest)
		}
	})

	var mutations int
	err := r.mutateProject(context.Background(), "p-1", func(doc map[string]any) (bool, error) {
		mutations++
		upsertProjectSecret(doc, "k", "v")
		return true, nil
	})
	if err != nil {
		t.Fatalf("mutateProject: %v", err)
	}
	if puts.Load() != 2 {
		t.Fatalf("expected 2 PUTs (conflict + retry), got %d", puts.Load())
	}
	if mutations != 2 {
		t.Fatalf("mutate must re-run on the re-read doc; ran %d times", mutations)
	}
}

// TestProjectSecret_mutateProjectGivesUpAfterMaxAttempts proves the retry loop
// is bounded rather than spinning forever on a persistent conflict.
func TestProjectSecret_mutateProjectGivesUpAfterMaxAttempts(t *testing.T) {
	var gets, puts atomic.Int64
	r, _ := newTestSecretResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			gets.Add(1)
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(`{"resourceType":"Project","id":"p-1","meta":{"versionId":"1"}}`))
		case http.MethodPut:
			puts.Add(1)
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"conflict","diagnostics":"Precondition Failed"}]}`))
		}
	})

	err := r.mutateProject(context.Background(), "p-1", func(doc map[string]any) (bool, error) {
		upsertProjectSecret(doc, "k", "v")
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error should mention the conflict: %v", err)
	}
	if puts.Load() != projectSecretMaxAttempts {
		t.Fatalf("expected %d PUT attempts, got %d", projectSecretMaxAttempts, puts.Load())
	}
}

// TestProjectSecret_mutateProjectAbortsOnMutateError proves a permanent
// failure from mutate (e.g. duplicate name on create) is not retried.
func TestProjectSecret_mutateProjectAbortsOnMutateError(t *testing.T) {
	var puts atomic.Int64
	r, _ := newTestSecretResource(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(`{"resourceType":"Project","id":"p-1","meta":{"versionId":"1"}}`))
		case http.MethodPut:
			puts.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	})

	wantErr := fmt.Errorf("already exists")
	err := r.mutateProject(context.Background(), "p-1", func(map[string]any) (bool, error) {
		return false, wantErr
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected mutate error to surface, got %v", err)
	}
	if puts.Load() != 0 {
		t.Fatalf("mutate error must abort before any PUT; got %d", puts.Load())
	}
}

// readProjectSecrets fetches the session project's secret[] as name -> entry.
func readProjectSecrets(t *testing.T, c *client.Client) map[string]map[string]any {
	t.Helper()
	ctx := context.Background()
	pid, err := c.CurrentProjectID(ctx)
	if err != nil {
		t.Fatalf("current project: %v", err)
	}
	out, err := c.FHIRRead(ctx, "Project", pid)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	secrets := map[string]map[string]any{}
	arr, _ := doc["secret"].([]any)
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			if name, _ := m["name"].(string); name != "" {
				secrets[name] = m
			}
		}
	}
	return secrets
}

func TestAccProjectSecret_lifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)

	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	name := "tf-acc-secret-" + suffix
	cfg := func(value string) string {
		return fmt.Sprintf(`
resource "medplum_project_secret" "test" {
  name         = %q
  value_string = %q
}`, name, value)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: cfg("v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_project_secret.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_project_secret.test", "project_id"),
					resource.TestCheckResourceAttr("medplum_project_secret.test", "name", name),
					resource.TestCheckResourceAttr("medplum_project_secret.test", "value_string", "v1"),
					func(_ *terraform.State) error {
						got := readProjectSecrets(t, c)
						if e := got[name]; e == nil || e["valueString"] != "v1" {
							return fmt.Errorf("server secret %q = %v, want valueString v1", name, e)
						}
						return nil
					},
				),
			},
			{Config: cfg("v1"), PlanOnly: true}, // no-op plan
			{ // in-place value update
				Config: cfg("v2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("medplum_project_secret.test", "value_string", "v2"),
					func(_ *terraform.State) error {
						got := readProjectSecrets(t, c)
						if e := got[name]; e == nil || e["valueString"] != "v2" {
							return fmt.Errorf("server secret %q = %v, want valueString v2", name, e)
						}
						return nil
					},
				),
			},
			{ // import by name
				ResourceName:      "medplum_project_secret.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccProjectSecret_parallelApplies proves parallel safety: five secrets in
// one config apply concurrently (Terraform's default parallelism covers 5) and
// all race on the single Project resource; the If-Match retry loop must land
// every one of them, and the parallel destroy must remove exactly those five
// while preserving an out-of-band sibling entry.
func TestAccProjectSecret_parallelApplies(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)

	// Seed a sibling entry outside Terraform; it must survive apply AND destroy.
	sentinel := "tf-acc-sentinel-" + suffix
	seedProjectSecret(t, c, sentinel, "keep-me")

	const n = 5
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, `
resource "medplum_project_secret" "s%d" {
  name         = "tf-acc-par-%s-%d"
  value_string = "value-%d"
}`, i, suffix, i, i)
	}

	checkAll := func(_ *terraform.State) error {
		got := readProjectSecrets(t, c)
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("tf-acc-par-%s-%d", suffix, i)
			e := got[name]
			if e == nil || e["valueString"] != fmt.Sprintf("value-%d", i) {
				return fmt.Errorf("secret %s missing or wrong after parallel apply: %v", name, e)
			}
		}
		if got[sentinel] == nil {
			return fmt.Errorf("out-of-band sibling secret %s lost during parallel apply", sentinel)
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			got := readProjectSecrets(t, c)
			for i := 0; i < n; i++ {
				name := fmt.Sprintf("tf-acc-par-%s-%d", suffix, i)
				if got[name] != nil {
					return fmt.Errorf("secret %s still present after parallel destroy", name)
				}
			}
			if got[sentinel] == nil {
				return fmt.Errorf("out-of-band sibling secret %s lost during parallel destroy", sentinel)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: sb.String(),
				Check:  resource.ComposeAggregateTestCheckFunc(checkAll),
			},
		},
	})
}

// TestAccProjectSecret_duplicateNameFails: creating a secret whose name already
// exists in Project.secret must fail instead of silently adopting the entry.
func TestAccProjectSecret_duplicateNameFails(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	name := "tf-acc-dup-" + suffix
	seedProjectSecret(t, c, name, "pre-existing")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "medplum_project_secret" "dup" {
  name         = %q
  value_string = "other"
}`, name),
				ExpectError: regexp.MustCompile(`already exists`),
			},
		},
	})
}

// seedProjectSecret writes an entry into Project.secret[] outside Terraform
// and registers cleanup.
func seedProjectSecret(t *testing.T, c *client.Client, name, value string) {
	t.Helper()
	ctx := context.Background()
	pid, err := c.CurrentProjectID(ctx)
	if err != nil {
		t.Fatalf("current project: %v", err)
	}
	mutate := func(change func(doc map[string]any)) {
		out, err := c.FHIRRead(ctx, "Project", pid)
		if err != nil {
			t.Fatalf("read project: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("decode project: %v", err)
		}
		change(doc)
		body, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.FHIRUpdate(ctx, "Project", pid, body); err != nil {
			t.Fatalf("write project: %v", err)
		}
	}
	mutate(func(doc map[string]any) { upsertProjectSecret(doc, name, value) })
	t.Cleanup(func() {
		mutate(func(doc map[string]any) { removeProjectSecret(doc, name) })
	})
}
