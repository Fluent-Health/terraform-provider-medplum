package fhirschema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed data/fhir.schema.json
var schemaBytes []byte

// Validator validates FHIR R4 resources against the embedded JSON schema.
type Validator struct {
	mu       sync.RWMutex
	compiler *jsonschema.Compiler
	cache    map[string]*jsonschema.Schema
	known    map[string]struct{} // resourceType -> presence check
}

const schemaURL = "file:///fhir.schema.json"

// New compiles the embedded schema and returns a reusable Validator.
func New() (*Validator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("parse embedded schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft6)
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}

	// Capture the set of known resource definitions for fast unknown-type errors.
	var raw struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal(schemaBytes, &raw); err != nil {
		return nil, fmt.Errorf("read definitions: %w", err)
	}
	known := make(map[string]struct{}, len(raw.Definitions))
	for k := range raw.Definitions {
		known[k] = struct{}{}
	}
	return &Validator{compiler: c, cache: map[string]*jsonschema.Schema{}, known: known}, nil
}

func (v *Validator) schemaFor(resourceType string) (*jsonschema.Schema, error) {
	v.mu.RLock()
	s, ok := v.cache[resourceType]
	v.mu.RUnlock()
	if ok {
		return s, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.cache[resourceType]; ok { // re-check under write lock
		return s, nil
	}
	if _, ok := v.known[resourceType]; !ok {
		return nil, fmt.Errorf("unknown FHIR resource type %q", resourceType)
	}
	s, err := v.compiler.Compile(schemaURL + "#/definitions/" + resourceType)
	if err != nil {
		return nil, fmt.Errorf("compile schema for %s: %w", resourceType, err)
	}
	v.cache[resourceType] = s
	return s, nil
}

// Validate checks that bodyJSON conforms to the schema for resourceType.
func (v *Validator) Validate(resourceType string, bodyJSON []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	s, err := v.schemaFor(resourceType)
	if err != nil {
		return err
	}
	if err := s.Validate(inst); err != nil {
		return fmt.Errorf("FHIR schema validation failed: %w", err)
	}
	return nil
}
