package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// ReadInputSizes obtains exact declared sizes through contract-scoped catalog
// authorization and repeats each lookup after the batch. It proves neither full
// dependency discovery nor final transaction stability; admission must lock pins.
func ReadInputSizes(ctx context.Context, catalog ArtifactCatalog, who auth.Identity, tenant uuid.UUID, nodes []LayerRef) ([]v2input.SemanticInputSize, error) {
	evidence, err := ReadInputEvidence(ctx, catalog, who, tenant, nodes)
	if err != nil {
		return nil, err
	}
	sizes := make([]v2input.SemanticInputSize, len(evidence))
	for i, entry := range evidence {
		sizes[i] = v2input.SemanticInputSize{Ref: entry.Ref, Bytes: entry.Object.Bytes}
	}
	return sizes, nil
}

// InputEvidence retains exact catalog metadata after repeated authorization.
// It is not an admission certificate: callers must lock original pins.
type InputEvidence struct {
	Ref      v2input.Ref
	Contract string
	Object   objectstore.Object
}

func ReadInputEvidence(ctx context.Context, catalog ArtifactCatalog, who auth.Identity, tenant uuid.UUID, nodes []LayerRef) ([]InputEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || len(nodes) == 0 || len(nodes) > 4097 {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(nodes)
	if err != nil || len(body) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	var owned []LayerRef
	if json.Unmarshal(body, &owned) != nil {
		return nil, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, node := range owned {
		if node.Ref.ArtifactID == uuid.Nil || node.Contract == "" || seen[node.Ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(node.Ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		seen[node.Ref.ArtifactID] = true
	}
	objects := make([]objectstore.Object, len(owned))
	evidence := make([]InputEvidence, len(owned))
	for i, node := range owned {
		object, err := catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return nil, err
		}
		if object.SHA256 != node.Ref.SHA256 || object.Bytes <= 0 || object.Bytes > 9007199254740991 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" {
			return nil, v2input.ErrInvalid
		}
		objects[i] = object
		evidence[i] = InputEvidence{Ref: node.Ref, Contract: node.Contract, Object: object}
	}
	for i, node := range owned {
		current, err := catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return nil, err
		}
		if current != objects[i] {
			return nil, v2input.ErrConflict
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return evidence, nil
}
