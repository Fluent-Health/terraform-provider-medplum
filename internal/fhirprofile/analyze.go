// Package fhirprofile statically classifies a FHIR R4 StructureDefinition against
// what Medplum actually enforces (verified vs @medplum/core v5.1.14). It reports
// REJECT (inert/throws in Medplum), WARN (accepted but silently unenforced), and
// counts ENFORCED constraints, so a profile that constrains nothing can fail loud.
package fhirprofile

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Severity classifies a finding.
type Severity int

const (
	// SeverityReject: Medplum would treat the construct as inert or throw on load.
	SeverityReject Severity = iota
	// SeverityWarn: Medplum accepts it but silently does not enforce it.
	SeverityWarn
)

// Finding is one classified construct.
type Finding struct {
	Severity Severity
	Path     string // element id/path
	Message  string
}

// Report is the result of analyzing one StructureDefinition.
type Report struct {
	Findings        []Finding
	EnforcedCount   int // number of genuinely-enforced constraints
	DecorativeCount int // count of decorative signals (mustSupport, required-binding, targetProfile, constraint)
}

// Rejects returns the REJECT findings.
func (r Report) Rejects() []Finding { return r.bySeverity(SeverityReject) }

// Warns returns the WARN findings.
func (r Report) Warns() []Finding { return r.bySeverity(SeverityWarn) }

func (r Report) bySeverity(s Severity) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == s {
			out = append(out, f)
		}
	}
	return out
}

// Summary is a one-line human summary for the plan output.
func (r Report) Summary() string {
	return fmt.Sprintf("%d enforced constraint(s), %d decorative signal(s), %d reject(s), %d warning(s)",
		r.EnforcedCount, r.DecorativeCount, len(r.Rejects()), len(r.Warns()))
}

type sdElement struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	SliceName   string `json:"sliceName"`
	Min         *int   `json:"min"`
	Max         string `json:"max"`
	MustSupport *bool  `json:"mustSupport"`
	Type        []struct {
		Code          string   `json:"code"`
		TargetProfile []string `json:"targetProfile"`
	} `json:"type"`
	Binding *struct {
		Strength string `json:"strength"`
		ValueSet string `json:"valueSet"`
	} `json:"binding"`
	Slicing *struct {
		Rules         string `json:"rules"`
		Ordered       *bool  `json:"ordered"`
		Discriminator []struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"discriminator"`
	} `json:"slicing"`
	Constraint []json.RawMessage `json:"constraint"`
}

func hasFixedOrPattern(raw map[string]json.RawMessage) bool {
	for k := range raw {
		if strings.HasPrefix(k, "fixed") || strings.HasPrefix(k, "pattern") {
			return true
		}
	}
	return false
}

// Analyze classifies the SD's snapshot elements. It returns an error only for
// malformed JSON; classification problems are returned as Findings.
func Analyze(sdJSON []byte) (Report, error) {
	var doc struct {
		Snapshot struct {
			Element []json.RawMessage `json:"element"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(sdJSON, &doc); err != nil {
		return Report{}, fmt.Errorf("invalid StructureDefinition JSON: %w", err)
	}

	var rep Report
	if len(doc.Snapshot.Element) == 0 {
		rep.Findings = append(rep.Findings, Finding{SeverityReject, "snapshot",
			"snapshot.element is empty: Medplum validates snapshot-only and treats a snapshot-less profile as inert"})
		return rep, nil
	}

	type parsedEl struct {
		el  sdElement
		raw map[string]json.RawMessage
	}
	all := make([]parsedEl, 0, len(doc.Snapshot.Element))
	fixedURLChild := map[string]bool{} // parent element id -> has a fixed/pattern `.url` child
	for _, rawEl := range doc.Snapshot.Element {
		var el sdElement
		if err := json.Unmarshal(rawEl, &el); err != nil {
			return Report{}, fmt.Errorf("invalid element: %w", err)
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(rawEl, &m)
		all = append(all, parsedEl{el, m})
		if strings.HasSuffix(el.ID, ".url") && hasFixedOrPattern(m) {
			fixedURLChild[strings.TrimSuffix(el.ID, ".url")] = true
		}
	}

	for _, p := range all {
		el := p.el

		// --- Slicing discriminators ---
		if el.Slicing != nil {
			for _, d := range el.Slicing.Discriminator {
				if d.Type != "value" && d.Type != "pattern" && d.Type != "type" {
					rep.reject(el.ID, fmt.Sprintf("unsupported slicing discriminator type %q: Medplum throws on load (only value/pattern/type)", d.Type))
				}
				if d.Path != "$this" && strings.ContainsAny(d.Path, "()") {
					rep.reject(el.ID, fmt.Sprintf("discriminator path %q uses FHIRPath functions: Medplum resolves only dotted paths + $this, so this slice never matches", d.Path))
				}
			}
			if r := el.Slicing.Rules; r == "closed" || r == "openAtEnd" {
				rep.warn(el.ID, fmt.Sprintf("slicing rule %q is parsed but NOT enforced by Medplum (all slicing behaves as open)", r))
			}
			if el.Slicing.Ordered != nil && *el.Slicing.Ordered {
				rep.warn(el.ID, "slicing 'ordered' is not enforced by Medplum")
			}
		}

		// --- Extension slices ---
		isExtSlice := el.SliceName != "" && strings.HasSuffix(el.Path, ".extension")
		if isExtSlice {
			if fixedURLChild[el.ID] {
				rep.EnforcedCount++ // extension presence + cardinality keyed by url IS enforced
			} else {
				rep.reject(el.ID, "extension slice has no fixed 'url' child: Medplum matches extension entries by url.fixed, so this slice never matches")
			}
		}

		// --- Enforced constraints ---
		// A valid extension slice already counted its presence+cardinality above;
		// don't double-count its min/max here (count once per logical constraint).
		if !isExtSlice && ((el.Min != nil && *el.Min > 0) || (el.Max != "" && el.Max != "*")) {
			rep.EnforcedCount++ // cardinality / required presence
		}
		// fixed[x]/pattern[x] on a non-url element (url fixed already counted via the extension slice)
		if hasFixedOrPattern(p.raw) && !strings.HasSuffix(el.ID, ".url") {
			rep.EnforcedCount++
		}

		// --- Decorative signals (parsed by Medplum, not enforced) ---
		if el.MustSupport != nil && *el.MustSupport {
			rep.DecorativeCount++
		}
		if el.Binding != nil && el.Binding.Strength == "required" && el.Binding.ValueSet != "" {
			rep.DecorativeCount++
		}
		for _, t := range el.Type {
			if len(t.TargetProfile) > 0 {
				rep.DecorativeCount++
				break
			}
		}
		if len(el.Constraint) > 0 {
			rep.DecorativeCount++
		}
	}
	return rep, nil
}

func (r *Report) reject(path, msg string) {
	r.Findings = append(r.Findings, Finding{SeverityReject, path, msg})
}

func (r *Report) warn(path, msg string) {
	r.Findings = append(r.Findings, Finding{SeverityWarn, path, msg})
}
