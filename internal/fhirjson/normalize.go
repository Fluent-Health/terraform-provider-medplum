package fhirjson

import (
	"bytes"
	"encoding/json"
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
