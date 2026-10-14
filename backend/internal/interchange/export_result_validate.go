package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
	"unicode/utf8"
)

type exportResultDocument struct {
	Schema   int           `json:"schema_version"`
	Report   v2input.Ref   `json:"report"`
	Bundle   *v2input.Ref  `json:"bundle"`
	Drafts   []v2input.Ref `json:"draft_shards"`
	Imported []v2input.Ref `json:"imported_files"`
}
type exportMappingField struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Reason *string `json:"reason"`
}
type exportMappingEntry struct {
	ID       uuid.UUID            `json:"entry_id"`
	External string               `json:"external_id"`
	Version  *uuid.UUID           `json:"asset_version_id"`
	Proposed *v2input.Ref         `json:"proposed_document"`
	Fields   []exportMappingField `json:"fields"`
}
type exportMappingShard struct {
	Schema  int                  `json:"schema_version"`
	Entries []exportMappingEntry `json:"entries"`
}
type exportMappingReport struct {
	Schema      int           `json:"schema_version"`
	Count       int           `json:"entry_count"`
	Shards      []v2input.Ref `json:"shards"`
	Rejected    int           `json:"rejected_count"`
	Unsupported int           `json:"unsupported_count"`
}

// ExportResultDocumentBytes are canonical, hash-pinned JSON receipts. Physical
// object verification, output task scope and current SQL authority are separate
// mandatory adoption gates; this validator never marks a run completed.
type ExportResultDocumentBytes struct {
	Ref  v2input.Ref
	Body []byte
}
type ValidatedExportResult struct {
	root, bundle, report v2input.Ref
	shards               []v2input.Ref
}

func (v ValidatedExportResult) Root() v2input.Ref   { return v.root }
func (v ValidatedExportResult) Bundle() v2input.Ref { return v.bundle }
func (v ValidatedExportResult) Documents() []v2input.Ref {
	return append([]v2input.Ref{v.root, v.report}, v.shards...)
}

func exactSidecarMappingFields(entry importPlanEntry, fields []exportMappingField) bool {
	expected := []exportMappingField{{Name: "rating", Status: "exact"}, {Name: "pick", Status: "exact"}, {Name: "orientation", Status: "exact"}}
	if entry.Crop != nil {
		expected = append(expected, exportMappingField{Name: "crop", Status: "exact"})
	}
	if entry.Label != nil {
		expected = append(expected, exportMappingField{Name: "label", Status: "exact"})
	}
	recipe := "xmp_subset_has_no_recipe_contract"
	raw := "renderer_specific_parameters_not_portable"
	expected = append(expected, exportMappingField{Name: "recipe", Status: "unsupported", Reason: &recipe}, exportMappingField{Name: "raw_development", Status: "unsupported", Reason: &raw})
	if len(fields) != len(expected) {
		return false
	}
	for i, want := range expected {
		got := fields[i]
		if got.Name != want.Name || got.Status != want.Status || (got.Reason == nil) != (want.Reason == nil) {
			return false
		}
		if want.Reason != nil && *got.Reason != *want.Reason {
			return false
		}
	}
	return true
}

// ValidateExportResultDocuments proves the bounded report inventory against the
// frozen plan. It deliberately does not interpret a JSON receipt as ZIP proof.
func ValidateExportResultDocuments(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, root, report ExportResultDocumentBytes, shards []ExportResultDocumentBytes) (ValidatedExportResult, error) {
	var zero ValidatedExportResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return zero, err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		return zero, err
	}
	forbidden := map[uuid.UUID]bool{}
	for _, entry := range compiled.roots {
		forbidden[entry.Ref.ArtifactID] = true
	}
	check := func(doc ExportResultDocumentBytes, target any) error {
		if !validImportRef(doc.Ref) || forbidden[doc.Ref.ArtifactID] || rawSHA(doc.Body) != doc.Ref.SHA256 {
			return v2input.ErrInvalid
		}
		forbidden[doc.Ref.ArtifactID] = true
		return decodeImportDocument(doc.Body, target)
	}
	var result exportResultDocument
	if err := check(root, &result); err != nil {
		return zero, err
	}
	if result.Schema != 1 || result.Bundle == nil || !validImportRef(*result.Bundle) || forbidden[result.Bundle.ArtifactID] || result.Drafts == nil || result.Imported == nil || len(result.Drafts) != 0 || len(result.Imported) != 0 || result.Report != report.Ref {
		return zero, v2input.ErrInvalid
	}
	forbidden[result.Bundle.ArtifactID] = true
	var mapping exportMappingReport
	if err := check(report, &mapping); err != nil {
		return zero, err
	}
	expected := append([]importPlanEntry(nil), plan.Entries...)
	sort.Slice(expected, func(i, j int) bool { return expected[i].ID.String() < expected[j].ID.String() })
	if mapping.Schema != 1 || mapping.Count != len(expected) || len(shards) != (len(expected)+99)/100 || len(mapping.Shards) != len(shards) || mapping.Rejected < 0 || mapping.Unsupported < 0 {
		return zero, v2input.ErrInvalid
	}
	if len(root.Body) > 64<<10 {
		return zero, v2input.ErrInvalid
	}
	total := len(report.Body)
	rejected, unsupported, cursor := 0, 0, 0
	refs := make([]v2input.Ref, 0, len(shards))
	for ordinal, doc := range shards {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		total += len(doc.Body)
		if total > 8<<20 || mapping.Shards[ordinal] != doc.Ref {
			return zero, v2input.ErrInvalid
		}
		var shard exportMappingShard
		if err := check(doc, &shard); err != nil {
			return zero, err
		}
		count := len(expected) - cursor
		if count > 100 {
			count = 100
		}
		if shard.Schema != 1 || len(shard.Entries) != count {
			return zero, v2input.ErrInvalid
		}
		for _, entry := range shard.Entries {
			want := expected[cursor]
			cursor++
			if entry.ID != want.ID || entry.External != want.External || want.Binding == nil || entry.Version == nil || *entry.Version != want.Binding.AssetVersionID || entry.Proposed != nil || entry.Fields == nil || len(entry.Fields) > 128 {
				return zero, v2input.ErrInvalid
			}
			names := map[string]bool{}
			hasRejected, hasUnsupported := false, false
			for _, field := range entry.Fields {
				if !utf8.ValidString(field.Name) || utf8.RuneCountInString(field.Name) < 1 || utf8.RuneCountInString(field.Name) > 80 || names[field.Name] {
					return zero, v2input.ErrInvalid
				}
				names[field.Name] = true
				switch field.Status {
				case "exact":
					if field.Reason != nil {
						return zero, v2input.ErrInvalid
					}
				case "unsupported", "rejected":
					if field.Reason == nil || !utf8.ValidString(*field.Reason) || utf8.RuneCountInString(*field.Reason) < 1 || utf8.RuneCountInString(*field.Reason) > 200 {
						return zero, v2input.ErrInvalid
					}
				default:
					return zero, v2input.ErrInvalid
				}
				hasRejected = hasRejected || field.Status == "rejected"
				hasUnsupported = hasUnsupported || field.Status == "unsupported"
			}
			if plan.Format != "photoeditor_bundle" && !exactSidecarMappingFields(want, entry.Fields) {
				return zero, v2input.ErrInvalid
			}
			if hasRejected {
				rejected++
			}
			if hasUnsupported {
				unsupported++
			}
		}
		refs = append(refs, doc.Ref)
	}
	if cursor != len(expected) || mapping.Rejected != rejected || mapping.Unsupported != unsupported || total > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	return ValidatedExportResult{root: root.Ref, bundle: *result.Bundle, report: report.Ref, shards: refs}, ctx.Err()
}
