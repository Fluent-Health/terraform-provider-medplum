package fhirmigrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// BuildScanQuery assembles the raw FHIR search query for one scan page:
// the caller's search, a `_tag:not` filter that excludes records already
// tagged at this migration+hash (self-limiting), and `_count`.
func BuildScanQuery(search, markerSystem, name, hash string, pageSize int) string {
	tagValue := markerSystem + "/" + name + "|" + hash
	parts := make([]string, 0, 3)
	if search != "" {
		parts = append(parts, search)
	}
	parts = append(parts, "_tag:not="+url.QueryEscape(tagValue))
	parts = append(parts, "_count="+strconv.Itoa(pageSize))
	return strings.Join(parts, "&")
}

// ParseSearchEntries returns the resources in a searchset bundle, skipping
// search-mode OperationOutcome entries.
func ParseSearchEntries(body []byte) ([]map[string]any, error) {
	var b struct {
		Entry []struct {
			Resource json.RawMessage `json:"resource"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("parse searchset bundle: %w", err)
	}
	out := make([]map[string]any, 0, len(b.Entry))
	for _, e := range b.Entry {
		if len(e.Resource) == 0 {
			continue
		}
		var res map[string]any
		if err := json.Unmarshal(e.Resource, &res); err != nil {
			return nil, fmt.Errorf("parse search entry resource: %w", err)
		}
		if rt, _ := res["resourceType"].(string); rt == "" || rt == "OperationOutcome" {
			continue
		}
		out = append(out, res)
	}
	return out, nil
}

// BuildBundle builds a batch/transaction bundle that PUTs each resource by id.
func BuildBundle(bundleType string, resources []map[string]any) ([]byte, error) {
	entries := make([]map[string]any, 0, len(resources))
	for _, res := range resources {
		rt, _ := res["resourceType"].(string)
		id, _ := res["id"].(string)
		if rt == "" || id == "" {
			return nil, fmt.Errorf("resource missing resourceType/id: %v", res)
		}
		entries = append(entries, map[string]any{
			"request":  map[string]any{"method": "PUT", "url": rt + "/" + id},
			"resource": res,
		})
	}
	return json.Marshal(map[string]any{
		"resourceType": "Bundle",
		"type":         bundleType,
		"entry":        entries,
	})
}

// BundleResult tallies per-entry outcomes of a batch/transaction response.
type BundleResult struct {
	Succeeded int
	Failed    int
}

// ParseBundleResponse counts 2xx entries as succeeded, everything else failed.
func ParseBundleResponse(body []byte) (BundleResult, error) {
	var b struct {
		Entry []struct {
			Response struct {
				Status string `json:"status"`
			} `json:"response"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return BundleResult{}, fmt.Errorf("parse bundle response: %w", err)
	}
	var r BundleResult
	for _, e := range b.Entry {
		if strings.HasPrefix(e.Response.Status, "2") {
			r.Succeeded++
		} else {
			r.Failed++
		}
	}
	return r, nil
}
