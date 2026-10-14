package interchange

import (
	"context"
	"io"
	"unicode/utf8"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type ImportAdoptionOutputCatalog interface {
	RootImportAdoption(context.Context, ImportAdoptionClaim, string) (v2input.Ref, objectstore.Object, error)
	LookupImportOutput(context.Context, ImportAdoptionClaim, v2input.Ref, string, string) (objectstore.Object, error)
	CurrentImportAdoptionClaim(context.Context, ImportAdoptionClaim) (bool, error)
}

// VerifiedImportResultBytes are physically read and structurally checked.
// Semantic reconstruction against Params and source metadata still follows.
type VerifiedImportResultBytes struct {
	Root, Report ExportResultDocumentBytes
	Shards       []ExportResultDocumentBytes
	Imported     []v2input.Ref
}

type ImportAdoptionResultReader struct {
	Catalog ImportAdoptionOutputCatalog
	Objects ImportDocumentObjects
	Bucket  string
}

func (r ImportAdoptionResultReader) Read(ctx context.Context, claim ImportAdoptionClaim) (VerifiedImportResultBytes, error) {
	var zero VerifiedImportResultBytes
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r.Catalog == nil || r.Objects == nil || claim.validate() != nil ||
		len(r.Bucket) < 3 || len(r.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	current, err := r.Catalog.CurrentImportAdoptionClaim(ctx, claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	rootRef, rootPin, err := r.Catalog.RootImportAdoption(ctx, claim, r.Bucket)
	if err != nil {
		return zero, err
	}
	type pinned struct {
		ref      v2input.Ref
		contract string
		object   objectstore.Object
	}
	pins := make([]pinned, 0, 52)
	seen := map[uuid.UUID]bool{}
	mappingBytes := int64(0)
	read := func(ref v2input.Ref, contract string, maximum int64) (ExportResultDocumentBytes, error) {
		var empty ExportResultDocumentBytes
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if !validImportRef(ref) || seen[ref.ArtifactID] {
			return empty, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
		pin, err := r.Catalog.LookupImportOutput(ctx, claim, ref, contract, r.Bucket)
		if err != nil {
			return empty, err
		}
		if pin.Key == "" || pin.VersionID == "" || pin.VersionID == "null" ||
			pin.ContentType != "application/json" || pin.SHA256 != ref.SHA256 ||
			pin.Bytes < 2 || pin.Bytes > maximum {
			return empty, v2input.ErrInvalid
		}
		if contract != "InterchangeResult" {
			mappingBytes += pin.Bytes
			if mappingBytes > 8<<20 {
				return empty, v2input.ErrInvalid
			}
		}
		stream, err := r.Objects.Open(ctx, pin)
		if err != nil {
			if stream != nil {
				_ = stream.Close()
			}
			return empty, err
		}
		if stream == nil {
			return empty, v2input.ErrInvalid
		}
		raw, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, pin.Bytes+1))
		closeErr := stream.Close()
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if readErr != nil {
			return empty, readErr
		}
		if closeErr != nil {
			return empty, closeErr
		}
		if int64(len(raw)) != pin.Bytes || rawSHA(raw) != ref.SHA256 {
			return empty, objectstore.ErrIntegrity
		}
		latest, err := r.Catalog.LookupImportOutput(ctx, claim, ref, contract, r.Bucket)
		if err != nil {
			return empty, err
		}
		if latest != pin {
			return empty, v2input.ErrInvalid
		}
		pins = append(pins, pinned{ref, contract, pin})
		return ExportResultDocumentBytes{Ref: ref, Body: raw}, nil
	}
	if !validImportRef(rootRef) || rootPin.SHA256 != rootRef.SHA256 {
		return zero, v2input.ErrInvalid
	}
	root, err := read(rootRef, "InterchangeResult", 65536)
	if err != nil {
		return zero, err
	}
	if pins[0].object != rootPin {
		return zero, v2input.ErrInvalid
	}
	var result exportResultDocument
	if err := decodeImportDocument(root.Body, &result); err != nil {
		return zero, err
	}
	if result.Schema != 1 || result.Bundle != nil || !validImportRef(result.Report) ||
		result.Drafts == nil || result.Imported == nil || len(result.Drafts) < 1 ||
		len(result.Drafts) > 50 || len(result.Imported) > 30000 {
		return zero, v2input.ErrInvalid
	}
	importedSeen := map[uuid.UUID]bool{}
	for _, ref := range result.Imported {
		if !validImportRef(ref) || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		importedSeen[ref.ArtifactID] = true
	}
	if importedSeen[result.Report.ArtifactID] {
		return zero, v2input.ErrInvalid
	}
	report, err := read(result.Report, "MappingReport", 8<<20)
	if err != nil {
		return zero, err
	}
	var mapping exportMappingReport
	if err := decodeImportDocument(report.Body, &mapping); err != nil {
		return zero, err
	}
	if mapping.Schema != 1 || mapping.Count < 1 || mapping.Count > 5000 ||
		mapping.Rejected < 0 || mapping.Unsupported < 0 ||
		len(mapping.Shards) != (mapping.Count+99)/100 ||
		len(mapping.Shards) != len(result.Drafts) {
		return zero, v2input.ErrInvalid
	}
	outputIDs := map[uuid.UUID]bool{rootRef.ArtifactID: true, result.Report.ArtifactID: true}
	for ordinal, ref := range mapping.Shards {
		if !validImportRef(ref) || ref != result.Drafts[ordinal] ||
			outputIDs[ref.ArtifactID] || importedSeen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		outputIDs[ref.ArtifactID] = true
	}
	shards := make([]ExportResultDocumentBytes, 0, len(mapping.Shards))
	entries, rejected, unsupported := 0, 0, 0
	entryIDs := map[uuid.UUID]bool{}
	externalIDs := map[string]bool{}
	versions := map[uuid.UUID]bool{}
	proposals := map[uuid.UUID]bool{}
	for ordinal, ref := range mapping.Shards {
		if ref != result.Drafts[ordinal] {
			return zero, v2input.ErrInvalid
		}
		doc, err := read(ref, "MappingShard", 8<<20)
		if err != nil {
			return zero, err
		}
		var shard exportMappingShard
		if err := decodeImportDocument(doc.Body, &shard); err != nil {
			return zero, err
		}
		want := mapping.Count - entries
		if want > 100 {
			want = 100
		}
		if shard.Schema != 1 || len(shard.Entries) != want {
			return zero, v2input.ErrInvalid
		}
		for _, entry := range shard.Entries {
			if entry.ID == uuid.Nil || entryIDs[entry.ID] ||
				!utf8.ValidString(entry.External) || utf8.RuneCountInString(entry.External) < 1 ||
				utf8.RuneCountInString(entry.External) > 240 || externalIDs[entry.External] ||
				len(entry.Fields) > 128 {
				return zero, v2input.ErrInvalid
			}
			entryIDs[entry.ID], externalIDs[entry.External] = true, true
			if entry.Version != nil {
				if *entry.Version == uuid.Nil || versions[*entry.Version] {
					return zero, v2input.ErrInvalid
				}
				versions[*entry.Version] = true
			}
			if entry.Proposed != nil {
				if entry.Version == nil || !validImportRef(*entry.Proposed) ||
					outputIDs[entry.Proposed.ArtifactID] || importedSeen[entry.Proposed.ArtifactID] ||
					proposals[entry.Proposed.ArtifactID] {
					return zero, v2input.ErrInvalid
				}
				proposals[entry.Proposed.ArtifactID] = true
			}
			names := map[string]bool{}
			hasRejected, hasUnsupported := false, false
			for _, field := range entry.Fields {
				if !utf8.ValidString(field.Name) || utf8.RuneCountInString(field.Name) < 1 ||
					utf8.RuneCountInString(field.Name) > 80 || names[field.Name] ||
					(field.Status != "exact" && field.Status != "unsupported" && field.Status != "rejected") ||
					(field.Status == "exact") != (field.Reason == nil) {
					return zero, v2input.ErrInvalid
				}
				if field.Reason != nil && (!utf8.ValidString(*field.Reason) ||
					utf8.RuneCountInString(*field.Reason) < 1 || utf8.RuneCountInString(*field.Reason) > 200) {
					return zero, v2input.ErrInvalid
				}
				names[field.Name] = true
				hasRejected = hasRejected || field.Status == "rejected"
				hasUnsupported = hasUnsupported || field.Status == "unsupported"
			}
			if hasRejected {
				rejected++
			}
			if hasUnsupported {
				unsupported++
			}
			entries++
		}
		shards = append(shards, doc)
	}
	if entries != mapping.Count || rejected != mapping.Rejected ||
		unsupported != mapping.Unsupported {
		return zero, v2input.ErrInvalid
	}
	for _, pin := range pins {
		latest, err := r.Catalog.LookupImportOutput(ctx, claim, pin.ref, pin.contract, r.Bucket)
		if err != nil {
			return zero, err
		}
		if latest != pin.object {
			return zero, v2input.ErrInvalid
		}
	}
	latestRoot, latestPin, err := r.Catalog.RootImportAdoption(ctx, claim, r.Bucket)
	if err != nil {
		return zero, err
	}
	current, err = r.Catalog.CurrentImportAdoptionClaim(ctx, claim)
	if err != nil {
		return zero, err
	}
	if !current || latestRoot != rootRef || latestPin != rootPin {
		return zero, v2input.ErrInvalid
	}
	return VerifiedImportResultBytes{Root: root, Report: report, Shards: shards,
		Imported: append([]v2input.Ref(nil), result.Imported...)}, ctx.Err()
}
