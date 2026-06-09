package fhirjson

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// Canonicalize re-encodes JSON with sorted keys (encoding/json sorts map keys),
// producing a byte-stable form for comparison.
//
// Numbers are decoded as float64; integers larger than 2^53 may lose precision.
// This is acceptable for FHIR drift comparison but unsuitable for exact numeric audit.
func Canonicalize(in []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// StripServerFields removes server-managed fields (top-level id, meta.versionId,
// meta.lastUpdated) and drops meta entirely if it becomes empty, then canonicalizes.
func StripServerFields(in []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(in, &m); err != nil {
		return nil, err
	}
	delete(m, "id")
	if m["meta"] == nil {
		delete(m, "meta")
	} else if meta, ok := m["meta"].(map[string]any); ok {
		delete(meta, "versionId")
		delete(meta, "lastUpdated")
		if len(meta) == 0 {
			delete(m, "meta")
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Equal reports whether two FHIR documents are equal after stripping
// server-managed fields and canonicalizing.
func Equal(a, b []byte) (bool, error) {
	na, err := StripServerFields(a)
	if err != nil {
		return false, err
	}
	nb, err := StripServerFields(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(na, nb), nil
}

// Contains reports whether the server document satisfies the config document:
// every field the user specified in config is present in server with an equal
// value. Fields present only in server (e.g. server-managed meta.project,
// meta.author, narrative text, defaults) are ignored. Arrays must match in
// length and be element-wise contained (the server does not pad user arrays).
func Contains(config, server []byte) (bool, error) {
	var c, s any
	if err := json.Unmarshal(config, &c); err != nil {
		return false, err
	}
	if err := json.Unmarshal(server, &s); err != nil {
		return false, err
	}
	return contains(c, s), nil
}

// isEmptyArray reports whether v is a JSON array with no elements.
func isEmptyArray(v any) bool {
	a, ok := v.([]any)
	return ok && len(a) == 0
}

func contains(c, s any) bool {
	switch cv := c.(type) {
	case map[string]any:
		sv, ok := s.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range cv {
			child, present := sv[k]
			if !present {
				// FHIR forbids empty arrays, so the server drops them on write.
				// An empty array in config therefore equals the server omitting
				// the field; treat it as satisfied rather than a spurious diff.
				if isEmptyArray(v) {
					continue
				}
				return false
			}
			if !contains(v, child) {
				return false
			}
		}
		return true
	case []any:
		// Order-insensitive containment: Medplum reorders array elements on write
		// (e.g. CodeSystem.concept, ValueSet.compose.include, extensions), so a
		// position-by-position comparison reports spurious diffs. Require every
		// config element to be satisfied by a distinct server element instead.
		// A genuinely changed element still matches nothing and surfaces a diff.
		sv, ok := s.([]any)
		if !ok || len(sv) != len(cv) {
			return false
		}
		used := make([]bool, len(sv))
		for _, ce := range cv {
			matched := false
			for i, se := range sv {
				if !used[i] && contains(ce, se) {
					used[i] = true
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(c, s)
	}
}
