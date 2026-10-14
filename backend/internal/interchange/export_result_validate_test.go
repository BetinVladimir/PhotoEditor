package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func exportResultJSON(t *testing.T, value any) ExportResultDocumentBytes {
	t.Helper()
	body, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return ExportResultDocumentBytes{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(body)}, Body: body}
}
func sidecarFieldsForTest(item importPlanEntry) []exportMappingField {
	fields := []exportMappingField{{Name: "rating", Status: "exact"}, {Name: "pick", Status: "exact"}, {Name: "orientation", Status: "exact"}}
	if item.Crop != nil {
		fields = append(fields, exportMappingField{Name: "crop", Status: "exact"})
	}
	if item.Label != nil {
		fields = append(fields, exportMappingField{Name: "label", Status: "exact"})
	}
	recipe := "xmp_subset_has_no_recipe_contract"
	raw := "renderer_specific_parameters_not_portable"
	return append(fields, exportMappingField{Name: "recipe", Status: "unsupported", Reason: &recipe}, exportMappingField{Name: "raw_development", Status: "unsupported", Reason: &raw})
}
func TestSidecarMappingFieldsBindOptionalColorLabel(t *testing.T) {
	item := importPlanEntry{}
	noLabel := sidecarFieldsForTest(item)
	if !exactSidecarMappingFields(item, noLabel) {
		t.Fatal("legacy sidecar fields refused")
	}
	label := "Blue"
	item.Label = &label
	withLabel := sidecarFieldsForTest(item)
	if !exactSidecarMappingFields(item, withLabel) || exactSidecarMappingFields(item, noLabel) {
		t.Fatal("color label report not pinned")
	}
	changed := append([]exportMappingField(nil), withLabel...)
	changed[3].Status = "unsupported"
	if exactSidecarMappingFields(item, changed) {
		t.Fatal("unapplied label accepted as exact")
	}
}
func TestExportResultDocumentsBindFrozenInventoryAndOwnReceipts(t *testing.T) {
	intent, compiled, snapshot, raw := exportFreezeFixture(t)
	frozen, err := FreezeExportDocuments(context.Background(), intent, compiled, snapshot, raw)
	if err != nil {
		t.Fatal(err)
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		t.Fatal(err)
	}
	item := plan.Entries[0]
	version := item.Binding.AssetVersionID
	shardValue := exportMappingShard{Schema: 1, Entries: []exportMappingEntry{{ID: item.ID, External: item.External, Version: &version, Fields: sidecarFieldsForTest(item)}}}
	shard := exportResultJSON(t, shardValue)
	reportValue := exportMappingReport{Schema: 1, Count: 1, Shards: []v2input.Ref{shard.Ref}, Unsupported: 1}
	report := exportResultJSON(t, reportValue)
	bundle := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("physical ZIP must be independently verified"))}
	rootValue := exportResultDocument{Schema: 1, Report: report.Ref, Bundle: &bundle, Drafts: []v2input.Ref{}, Imported: []v2input.Ref{}}
	root := exportResultJSON(t, rootValue)
	receipt, err := ValidateExportResultDocuments(context.Background(), intent, frozen, root, report, []ExportResultDocumentBytes{shard})
	if err != nil || receipt.Root() != root.Ref || receipt.Bundle() != bundle || len(receipt.Documents()) != 3 {
		t.Fatal("valid report refused", err)
	}
	refs := receipt.Documents()
	refs[0] = v2input.Ref{}
	shard.Body[0] = 'x'
	if receipt.Root() != root.Ref || receipt.Documents()[2] != shard.Ref {
		t.Fatal("receipt aliases caller buffers")
	}
}
func TestExportResultDocumentsRejectInventoriesAndInvalidStatus(t *testing.T) {
	intent, compiled, snapshot, raw := exportFreezeFixture(t)
	frozen, err := FreezeExportDocuments(context.Background(), intent, compiled, snapshot, raw)
	if err != nil {
		t.Fatal(err)
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		t.Fatal(err)
	}
	item := plan.Entries[0]
	for _, kind := range []string{"version", "entry", "external", "proposal", "duplicate_field", "reason", "status", "counts", "missing_label", "missing_orientation", "wrong_recipe_reason", "null_drafts", "imported", "bundle_alias", "missing_shard", "hash"} {
		t.Run(kind, func(t *testing.T) {
			version := item.Binding.AssetVersionID
			entry := exportMappingEntry{ID: item.ID, External: item.External, Version: &version, Fields: sidecarFieldsForTest(item)}
			switch kind {
			case "version":
				version = uuid.New()
			case "entry":
				entry.ID = uuid.New()
			case "external":
				entry.External = "wrong"
			case "proposal":
				entry.Proposed = &compiled.paramsRef
			case "duplicate_field":
				entry.Fields = append(entry.Fields, entry.Fields[0])
			case "reason":
				reason := "unexpected"
				entry.Fields[0].Reason = &reason
			case "status":
				entry.Fields[0].Status = "unknown"
			case "missing_label":
				entry.Fields = append(entry.Fields, exportMappingField{Name: "label", Status: "exact"})
			case "missing_orientation":
				entry.Fields = append(entry.Fields[:2], entry.Fields[3:]...)
			case "wrong_recipe_reason":
				reason := "wrong"
				entry.Fields[len(entry.Fields)-2].Reason = &reason
			}
			shard := exportResultJSON(t, exportMappingShard{Schema: 1, Entries: []exportMappingEntry{entry}})
			mapping := exportMappingReport{Schema: 1, Count: 1, Shards: []v2input.Ref{shard.Ref}, Unsupported: 1}
			if kind == "counts" {
				mapping.Unsupported = 0
			}
			report := exportResultJSON(t, mapping)
			bundle := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("zip"))}
			result := exportResultDocument{Schema: 1, Report: report.Ref, Bundle: &bundle, Drafts: []v2input.Ref{}, Imported: []v2input.Ref{}}
			if kind == "null_drafts" {
				result.Drafts = nil
			}
			if kind == "imported" {
				result.Imported = []v2input.Ref{compiled.paramsRef}
			}
			if kind == "bundle_alias" {
				bundle = report.Ref
			}
			root := exportResultJSON(t, result)
			shards := []ExportResultDocumentBytes{shard}
			if kind == "missing_shard" {
				shards = nil
			}
			if kind == "hash" {
				root.Body[0] = 'x'
			}
			if _, err := ValidateExportResultDocuments(context.Background(), intent, frozen, root, report, shards); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateExportResultDocuments(ctx, intent, frozen, ExportResultDocumentBytes{}, ExportResultDocumentBytes{}, nil); err == nil {
		t.Fatal("cancel accepted")
	}
}
