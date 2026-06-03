package provider

import "testing"

func TestProject_toFHIR(t *testing.T) {
	m := projectModel{
		Name:        typesStr("Acme"),
		Description: typesStr("d"),
		Features:    stringsToList([]string{"bots"}),
	}
	b, err := m.toFHIR("p1")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(b), `"id":"p1"`) || !contains(string(b), `"bots"`) {
		t.Fatalf("unexpected body: %s", b)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
