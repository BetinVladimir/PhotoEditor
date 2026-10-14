package provenance

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Every JSON dependency must be physically witnessed, independently of whether
// it changes pixels, controls a mask or contributes authoring metadata.
type signingRenderDocumentOccurrenceKey struct {
	Path, Contract string
	Ref, Parent    v2input.Ref
	Object         objectstore.Object
}

func verifySigningRenderDocumentCoverage(evidence SigningRenderClosureEvidence) error {
	entries := map[uuid.UUID]v2input.Entry{}
	documents := map[uuid.UUID]bool{}
	witnesses := map[uuid.UUID]string{}
	objects := map[uuid.UUID]objectstore.Object{}
	if len(evidence.inventory.Entries) < 2 || len(evidence.inventory.Entries) > 4096 {
		return ErrInvalid
	}
	for _, entry := range evidence.inventory.Entries {
		if !ref(entry.Ref) || entry.Contract == "" {
			return ErrInvalid
		}
		if _, exists := entries[entry.Ref.ArtifactID]; exists {
			return ErrInvalid
		}
		entries[entry.Ref.ArtifactID] = entry
	}
	occurrences := map[signingRenderDocumentOccurrenceKey]bool{}
	witnessedOccurrences := map[signingRenderDocumentOccurrenceKey]bool{}
	for _, occurrence := range evidence.inventory.Occurrences {
		node := occurrence.Node
		if occurrence.Object.ContentType != "application/json" {
			continue
		}
		entry, exists := entries[node.Ref.ArtifactID]
		if !exists || entry.Ref != node.Ref || (entry.Contract != node.Contract && node.Contract != "Artifact") {
			return ErrInvalid
		}
		occurrences[signingRenderDocumentOccurrenceKey{node.Path, node.Contract, node.Ref, node.ParentRef, occurrence.Object}] = true
	}
	for _, document := range evidence.inventory.Documents {
		entry, exists := entries[document.Ref.ArtifactID]
		if !exists || entry.Ref != document.Ref || (entry.Contract != document.Contract && document.Contract != "Artifact") || document.Contract == "NpyArray" || document.Contract == "StyleBackgroundAsset" {
			return ErrInvalid
		}
		key := signingRenderDocumentOccurrenceKey{document.Path, document.Contract, document.Ref, document.Parent, document.Object}
		if !occurrences[key] {
			return ErrInvalid
		}
		witnessedOccurrences[key] = true
		if !pinned([]byte(document.JCS), document.Ref, 8<<20) {
			return ErrInvalid
		}
		if document.Object.SHA256 != document.Ref.SHA256 || document.Object.ContentType != "application/json" || document.Object.Bytes != int64(len(document.JCS)) || document.Object.Key == "" || document.Object.VersionID == "" || document.Object.VersionID == "null" {
			return ErrInvalid
		}
		if documents[document.Ref.ArtifactID] && (witnesses[document.Ref.ArtifactID] != document.JCS || objects[document.Ref.ArtifactID] != document.Object) {
			return ErrInvalid
		}
		witnesses[document.Ref.ArtifactID] = document.JCS
		objects[document.Ref.ArtifactID] = document.Object
		documents[document.Ref.ArtifactID] = true
	}
	for _, entry := range evidence.inventory.Entries {
		switch entry.Contract {
		case "NpyArray", "StyleBackgroundAsset":
			continue
		default:
			if !documents[entry.Ref.ArtifactID] {
				return ErrInvalid
			}
		}
	}
	if len(witnessedOccurrences) != len(occurrences) {
		return ErrInvalid
	}
	return nil
}
