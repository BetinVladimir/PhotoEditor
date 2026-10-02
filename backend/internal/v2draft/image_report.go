package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"unicode/utf8"
)

func imageReportRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, ok := exactFields(raw, "schema_version", "status", "operation", "skipped_count", "reason", "outside_mask_max_error", "clipping_fraction", "identity", "transform", "sources", "registrations")
	if !ok {
		f, ok = exactFields(raw, "schema_version", "status", "operation", "skipped_count", "reason", "outside_mask_max_error", "clipping_fraction", "identity", "transform", "sources", "registrations", "parent_reports")
	}
	invalid := func() ([]LayerRef, error) { return nil, v2input.ErrInvalid }
	if !ok || !isDocumentRef(parent) {
		return invalid()
	}
	var version, skipped int
	var status, operation string
	if json.Unmarshal(f["schema_version"], &version) != nil || version != 1 || bytes.Equal(bytes.TrimSpace(f["skipped_count"]), []byte("null")) || json.Unmarshal(f["skipped_count"], &skipped) != nil || skipped < 0 || skipped > 5000 || json.Unmarshal(f["status"], &status) != nil || (status != "pass" && status != "review" && status != "fail" && status != "unavailable") || json.Unmarshal(f["operation"], &operation) != nil || utf8.RuneCountInString(operation) < 1 || utf8.RuneCountInString(operation) > 80 {
		return invalid()
	}
	if !bytes.Equal(bytes.TrimSpace(f["reason"]), []byte("null")) {
		var reason string
		if json.Unmarshal(f["reason"], &reason) != nil || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 200 {
			return invalid()
		}
	}
	for name, limit := range map[string]float64{"outside_mask_max_error": 1000000, "clipping_fraction": 1} {
		var value float64
		if bytes.Equal(bytes.TrimSpace(f[name]), []byte("null")) || json.Unmarshal(f[name], &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > limit {
			return invalid()
		}
	}
	children := []LayerRef{}
	for _, field := range []struct{ name, contract string }{{"identity", "IdentityReport"}, {"transform", "Artifact"}} {
		if bytes.Equal(bytes.TrimSpace(f[field.name]), []byte("null")) {
			continue
		}
		ref, valid := layerRef(f[field.name])
		if !valid {
			return invalid()
		}
		children = append(children, LayerRef{Path: path + "." + field.name, Contract: field.contract, Ref: ref, ParentRef: parent})
	}
	var sources, registrations []json.RawMessage
	if json.Unmarshal(f["sources"], &sources) != nil || len(sources) < 1 || len(sources) > 30 || json.Unmarshal(f["registrations"], &registrations) != nil || registrations == nil || len(registrations) > 30 {
		return invalid()
	}
	for i, raw := range sources {
		fields, valid := exactFields(raw, "asset_version_id", "source", "canvas_hash")
		if !valid {
			return invalid()
		}
		var binding v2input.SessionVersionBinding
		if json.Unmarshal(raw, &binding) != nil || binding.AssetVersionID == uuid.Nil || !isDocumentRef(binding.Source) || !isDocumentRef(v2input.Ref{ArtifactID: binding.AssetVersionID, SHA256: binding.CanvasHash}) {
			return invalid()
		}
		if _, valid = layerRef(fields["source"]); !valid {
			return invalid()
		}
		children = append(children, LayerRef{Path: path + ".sources[" + strconv.Itoa(i) + "].source", Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash, ParentRef: parent})
	}
	for i, raw := range registrations {
		ref, valid := layerRef(raw)
		if !valid {
			return invalid()
		}
		children = append(children, LayerRef{Path: path + ".registrations[" + strconv.Itoa(i) + "]", Contract: "Artifact", Ref: ref, ParentRef: parent})
	}
	if rawParents, exists := f["parent_reports"]; exists {
		var parents []json.RawMessage
		if json.Unmarshal(rawParents, &parents) != nil || parents == nil || len(parents) > 33 {
			return invalid()
		}
		seen := map[uuid.UUID]bool{}
		for i, raw := range parents {
			ref, valid := layerRef(raw)
			if !valid || seen[ref.ArtifactID] || ref == parent {
				return invalid()
			}
			seen[ref.ArtifactID] = true
			children = append(children, LayerRef{Path: path + ".parent_reports[" + strconv.Itoa(i) + "]", Contract: "ImageReport", Ref: ref, ParentRef: parent})
		}
	}
	return children, nil
}
