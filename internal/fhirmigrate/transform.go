package fhirmigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Coding is a FHIR Coding subset used for matching and rewriting.
type Coding struct {
	System  string
	Code    string
	Display string
}

// Remap rewrites any Coding matching From into To.
type Remap struct {
	From Coding
	To   Coding
}

// Spec is the transform-relevant description of a migration. It intentionally
// excludes execution knobs (bundle type, page size): those must not change the
// marker hash, so re-batching an already-migrated dataset is a no-op.
type Spec struct {
	TargetResourceType string
	Search             string
	MarkerSystem       string
	Remaps             []Remap
}

// ApplyRemaps recursively walks resource and rewrites every object with a
// string `code` matching a remap's From. The From's system must match too: a
// non-empty From.System matches only a coding carrying that exact system, and
// an empty From.System matches a coding whose system is absent or empty.
// Returns whether anything changed. Fixed point: re-applying yields no change.
func ApplyRemaps(resource map[string]any, remaps []Remap) bool {
	return applyNode(resource, remaps)
}

func applyNode(node any, remaps []Remap) bool {
	changed := false
	switch v := node.(type) {
	case map[string]any:
		if code, ok := v["code"].(string); ok {
			// system may be absent; treat that as the empty string so a remap
			// with From.System == "" matches a Coding that carries no system.
			sys, _ := v["system"].(string)
			for _, rm := range remaps {
				if code == rm.From.Code && sys == rm.From.System {
					v["system"] = rm.To.System
					v["code"] = rm.To.Code
					if rm.To.Display != "" {
						v["display"] = rm.To.Display
					}
					changed = true
					break
				}
			}
		}
		for _, val := range v {
			if applyNode(val, remaps) {
				changed = true
			}
		}
	case []any:
		for _, item := range v {
			if applyNode(item, remaps) {
				changed = true
			}
		}
	}
	return changed
}

// SetMarkerTag ensures resource.meta.tag carries exactly one tag with the given
// system (updating its code), preserving tags from other systems.
func SetMarkerTag(resource map[string]any, system, code string) {
	meta, _ := resource["meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		resource["meta"] = meta
	}
	var tags []any
	if existing, ok := meta["tag"].([]any); ok {
		for _, tv := range existing {
			if tm, ok := tv.(map[string]any); ok {
				if s, _ := tm["system"].(string); s == system {
					continue // drop our prior tag; we re-add current below
				}
			}
			tags = append(tags, tv)
		}
	}
	tags = append(tags, map[string]any{"system": system, "code": code})
	meta["tag"] = tags
}

// SpecHash returns a stable short hash of the transform spec.
func SpecHash(s Spec) string {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}
