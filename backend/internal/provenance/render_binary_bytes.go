package provenance

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Physical hashes establish immutable leaf identity. They do not replace NPY
// shape/dtype/value checks or replay the render's numerical computation.
func verifySigningRenderBinaryBytes(ctx context.Context, catalog v2draft.ArtifactCatalog, objects SigningObjects, who auth.Identity, ready ReadySigningExport, evidence SigningRenderClosureEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pins := map[uuid.UUID]v2input.Ref{}
	seen := map[uuid.UUID]bool{}
	hashed := map[uuid.UUID]objectstore.Object{}
	for _, entry := range evidence.inventory.Entries {
		if entry.Contract != "NpyArray" {
			continue
		}
		if !ref(entry.Ref) {
			return ErrInvalid
		}
		if _, exists := pins[entry.Ref.ArtifactID]; exists {
			return ErrInvalid
		}
		pins[entry.Ref.ArtifactID] = entry.Ref
	}
	for _, occurrence := range evidence.inventory.Occurrences {
		node := occurrence.Node
		switch node.Contract {
		case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
		default:
			continue
		}
		if catalog == nil || objects == nil || pins[node.Ref.ArtifactID] != node.Ref || !ref(node.ParentRef) {
			return ErrInvalid
		}
		raw, err := canonical.Marshal(node)
		if err != nil || len(raw) > 1<<20 {
			return ErrInvalid
		}
		lookup := func() (objectstore.Object, error) {
			var isolated v2draft.LayerRef
			if json.Unmarshal(raw, &isolated) != nil {
				return objectstore.Object{}, ErrInvalid
			}
			return catalog.Lookup(ctx, who, ready.Tenant, isolated)
		}
		object, err := lookup()
		if err != nil {
			return err
		}
		if object != occurrence.Object || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != node.Ref.SHA256 || (object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream") {
			return ErrInvalid
		}
		if prior, exists := hashed[node.Ref.ArtifactID]; exists {
			if prior != object {
				return ErrInvalid
			}
		} else {
			// Match the exact-integer binary length envelope. Memory remains a bounded
			// copy buffer; cancellation bounds the duration of external reads.
			if err := hashSigningRenderObject(ctx, objects, object, 9007199254740991); err != nil {
				return err
			}
			hashed[node.Ref.ArtifactID] = object
		}
		current, err := lookup()
		if err != nil {
			return err
		}
		if current != object {
			return ErrInvalid
		}
		seen[node.Ref.ArtifactID] = true
	}
	if len(seen) != len(pins) {
		return ErrInvalid
	}
	return ctx.Err()
}
