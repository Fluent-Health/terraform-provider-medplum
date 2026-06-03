package provider

import "testing"

func TestSplitRef(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
		wantID   string
		wantOK   bool
	}{
		{"ValueSet/abc123", "ValueSet", "abc123", true},
		{"abc123", "", "", false},
		{"/abc", "", "", false},
		{"ValueSet/", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		gotType, gotID, gotOK := splitRef(c.in)
		if gotOK != c.wantOK || (c.wantOK && (gotType != c.wantType || gotID != c.wantID)) {
			t.Errorf("splitRef(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, gotType, gotID, gotOK, c.wantType, c.wantID, c.wantOK)
		}
	}
}
