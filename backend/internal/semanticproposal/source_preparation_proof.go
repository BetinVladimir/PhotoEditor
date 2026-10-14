package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Value-owned selected source closure. ICC SHA is from the server registry;
// proof does not certify registry file immutability or grant queued admission.
type sourcePreparationProof struct {
	tenant, project, actor, session uuid.UUID
	bucket                          string
	binding                         v2input.SessionVersionBinding
	iccSHA                          string
	unitRGB                         bool
	pins                            string
}
type sourcePreparationPin struct {
	Node   v2draft.LayerRef
	Object objectstore.Object
}

func (s SQLTransferSourceReader) sourcePreparationPins(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile) (string, error) {
	body, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return "", err
	}
	if err := color.VerifyWorkingImage(body); err != nil {
		return "", err
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, body)
	if err != nil {
		return "", err
	}
	nodes := []v2draft.LayerRef{{Ref: binding.Source, Contract: "WorkingImage", CanvasHash: binding.CanvasHash}, {Ref: metadata.Inputs.Pixels, Contract: "WorkingPixels", ParentRef: binding.Source}}
	if metadata.Inputs.Alpha != nil {
		nodes = append(nodes, v2draft.LayerRef{Ref: *metadata.Inputs.Alpha, Contract: "WorkingAlpha", ParentRef: binding.Source})
	}
	fields, err := v2input.ResolveSemanticGeometryArrays(binding, body)
	if err != nil {
		return "", err
	}
	for _, field := range fields {
		nodes = append(nodes, v2draft.LayerRef{Ref: field.Ref, Contract: "DenseWarpField", ParentRef: binding.Source})
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	pins := make([]sourcePreparationPin, 0, len(nodes))
	for _, node := range nodes {
		object, err := catalog.Lookup(ctx, who, tenantID, node)
		if err != nil {
			return "", err
		}
		pins = append(pins, sourcePreparationPin{node, object})
	}
	raw, err := canonical.Marshal(pins)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
func (s SQLTransferSourceReader) readQualifiedSourcePreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, unitRGB bool) (v2input.SemanticSourceMetadata, sourcePreparationProof, error) {
	fail := func(err error) (v2input.SemanticSourceMetadata, sourcePreparationProof, error) {
		return v2input.SemanticSourceMetadata{}, sourcePreparationProof{}, err
	}
	before, err := s.sourcePreparationPins(ctx, who, tenantID, binding, color)
	if err != nil {
		return fail(err)
	}
	metadata, err := s.verifyQualifiedSourcePixels(ctx, who, tenantID, binding, color, unitRGB)
	if err != nil {
		return fail(err)
	}
	proof := sourcePreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, iccSHA: color.sha, unitRGB: unitRGB, pins: before}
	if err := s.recheckSourcePreparationProof(ctx, who, tenantID, binding, color, unitRGB, proof); err != nil {
		return fail(err)
	}
	return metadata, proof, nil
}
func (s SQLTransferSourceReader) recheckSourcePreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, unitRGB bool, p sourcePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || binding.AssetVersionID == uuid.Nil || p.tenant != tenantID || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != binding || p.iccSHA != color.sha || p.unitRGB != unitRGB || p.pins == "" {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return v2input.ErrInvalid
	}
	if _, err := decodeSourcePreparationPins(p); err != nil {
		return err
	}
	current, err := s.sourcePreparationPins(ctx, who, tenantID, binding, color)
	if err != nil {
		return err
	}
	if current != p.pins {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Validates only the privately retained closure before any SQL callback. Source
// content determines completeness; this decoder cannot replace discovery.
func decodeSourcePreparationPins(p sourcePreparationProof) ([]sourcePreparationPin, error) {
	if len(p.pins) < 2 || len(p.pins) > 4194304 {
		return nil, v2input.ErrInvalid
	}
	var pins []sourcePreparationPin
	if json.Unmarshal([]byte(p.pins), &pins) != nil || len(pins) < 2 {
		return nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(pins)
	if err != nil || string(raw) != p.pins {
		return nil, v2input.ErrInvalid
	}
	for i, pin := range pins {
		node, obj := pin.Node, pin.Object
		if node.Path != "" || node.ExpectedBinding != nil || node.ExpectedLearning != nil || node.ExpectedModelLockSHA != "" || node.ExpectedBackground != nil || node.RenderFrame != nil || node.Ref.ArtifactID == uuid.Nil || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.SHA256 != node.Ref.SHA256 || obj.Bytes < 2 {
			return nil, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(node.Ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		if i == 0 {
			if node.Contract != "WorkingImage" || node.Ref != p.binding.Source || node.CanvasHash != p.binding.CanvasHash || node.ParentRef != (v2input.Ref{}) || obj.ContentType != "application/json" || obj.Bytes > 1048576 {
				return nil, v2input.ErrInvalid
			}
		} else {
			if node.ParentRef != p.binding.Source || node.CanvasHash != "" || node.Ref.ArtifactID == p.binding.Source.ArtifactID || obj.Bytes < 32 || (obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream") {
				return nil, v2input.ErrInvalid
			}
			if i == 1 {
				if node.Contract != "WorkingPixels" {
					return nil, v2input.ErrInvalid
				}
			} else if node.Contract != "DenseWarpField" && !(i == 2 && node.Contract == "WorkingAlpha") {
				return nil, v2input.ErrInvalid
			}
		}
	}
	return pins, nil
}
