package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// verifyPerspectiveResultGeometry binds the reviewed result to its accepted
// source and to the exact final integer-center transform in WorkingImage.
func verifyPerspectiveResultGeometry(binding AdoptedPerspectiveBinding, result v2draft.ImageResultDocument, sourceRaw []byte, documents map[string][]byte) error {
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return err
	}
	sourceHash, err := canonical.Hash(source)
	if err != nil || sourceHash != binding.Base.Source.SHA256 {
		return v2input.ErrInvalid
	}
	sourceCanvas, err := canonical.Hash(source.Canvas)
	if err != nil || sourceCanvas != binding.Base.CanvasHash {
		return v2input.ErrInvalid
	}
	if result.GeneratedMask != nil {
		return v2input.ErrInvalid
	}
	targetRaw := documents[result.WorkingImage.ArtifactID.String()]
	if result.WorkingImage == binding.Base.Source {
		targetRaw = sourceRaw
	}
	target, err := review.ParseWorkingImage(targetRaw)
	if err != nil {
		return err
	}
	if target.Color != source.Color || target.DType != "float32" || target.Canvas.SourceSHA256 != source.Canvas.SourceSHA256 {
		return v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(documents[result.ChangedMask.ArtifactID.String()])
	if err != nil {
		return err
	}
	if mask.Canvas != target.Canvas || mask.Selector != "changed" || mask.Protected || mask.ObjectID != nil || mask.ModelLockID != nil {
		return v2input.ErrInvalid
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return err
	}
	targetChain, err := canonical.Bytes(target.TransformChain)
	if err != nil || !bytes.Equal(maskChain, targetChain) {
		return v2input.ErrInvalid
	}
	var report struct {
		Operation     string                          `json:"operation"`
		Status        string                          `json:"status"`
		Reason        *string                         `json:"reason"`
		Sources       []v2input.SessionVersionBinding `json:"sources"`
		Transform     *v2input.Ref                    `json:"transform"`
		Identity      *v2input.Ref                    `json:"identity"`
		Registrations []v2input.Ref                   `json:"registrations"`
	}
	if json.Unmarshal(documents[result.Report.ArtifactID.String()], &report) != nil || report.Operation != "optical.perspective" || len(report.Sources) != 1 || report.Sources[0] != binding.Base || report.Identity != nil || len(report.Registrations) != 0 {
		return v2input.ErrInvalid
	}
	if result.WorkingImage == binding.Base.Source {
		if report.Status != "pass" || report.Transform != nil || report.Reason != nil {
			return v2input.ErrInvalid
		}
		return nil
	}
	if report.Status != "review" || report.Transform == nil || report.Reason == nil || (*report.Reason != "perspective_manual" && *report.Reason != "perspective_auto") || target.Alpha == nil || target.Canvas.Orientation != 1 {
		return v2input.ErrInvalid
	}
	transformRaw := documents[report.Transform.ArtifactID.String()]
	if err := validatePerspectiveResolvedTransform(transformRaw); err != nil {
		return err
	}
	var transform struct {
		From   string    `json:"from_canvas_hash"`
		To     string    `json:"to_canvas_hash"`
		Matrix []float64 `json:"matrix"`
	}
	if json.Unmarshal(transformRaw, &transform) != nil {
		return v2input.ErrInvalid
	}
	targetCanvas, err := canonical.Hash(target.Canvas)
	if err != nil || transform.From != sourceCanvas || transform.To != targetCanvas {
		return v2input.ErrInvalid
	}
	var before, after struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if json.Unmarshal(source.TransformChain, &before) != nil || json.Unmarshal(target.TransformChain, &after) != nil || len(after.Steps) != len(before.Steps)+1 {
		return v2input.ErrInvalid
	}
	for i := range before.Steps {
		a, err := canonical.Bytes(before.Steps[i])
		if err != nil {
			return err
		}
		b, err := canonical.Bytes(after.Steps[i])
		if err != nil || !bytes.Equal(a, b) {
			return v2input.ErrInvalid
		}
	}
	var last struct {
		Kind          string                              `json:"kind"`
		From          review.Canvas                       `json:"from_canvas"`
		To            review.Canvas                       `json:"to_canvas"`
		Invertibility string                              `json:"invertibility"`
		Matrix        []float64                           `json:"forward_matrix"`
		Region        struct{ X, Y, Width, Height int64 } `json:"valid_region"`
	}
	if json.Unmarshal(after.Steps[len(before.Steps)], &last) != nil || last.Kind != "matrix" || last.From != source.Canvas || last.To != target.Canvas || last.Invertibility != "partial" || len(last.Matrix) != 9 || last.Region.X != 0 || last.Region.Y != 0 || last.Region.Width != target.Canvas.Width || last.Region.Height != target.Canvas.Height {
		return v2input.ErrInvalid
	}
	for i := range last.Matrix {
		if last.Matrix[i] != transform.Matrix[i] {
			return v2input.ErrInvalid
		}
	}
	return nil
}

func readPerspectiveGeometrySource(ctx context.Context, catalog v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant uuid.UUID, ref v2input.Ref) ([]byte, error) {
	if catalog == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	node := v2draft.LayerRef{Ref: ref, Contract: "WorkingImage", Path: "Original"}
	object, err := catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	if object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > 65536 || object.SHA256 != ref.SHA256 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" {
		return nil, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := readAdoptedRoot(ctx, reader, object.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum := sha256.Sum256(raw)
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	current, err := catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	if current.Key != object.Key || current.VersionID != object.VersionID || current.Bytes != object.Bytes || current.SHA256 != object.SHA256 || current.ContentType != object.ContentType {
		return nil, v2input.ErrExpired
	}
	return raw, nil
}
