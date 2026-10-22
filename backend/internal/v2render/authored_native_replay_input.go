package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredNativeReplayCatalog reads only this exact current request's
// canonical native producer inventory and actor-bound parent object scope.
// RequestJCS is owned by NewAuthoredNativeReplayCatalog before any IO.
type SQLAuthoredNativeReplayCatalog struct {
	Scheduler       *pgxpool.Pool
	Objects         v2input.SessionAnalysisObjectReader
	Bucket          string
	request         v2input.AuthoredNativeReplayRequest
	raw             string
	originalEntries map[v2input.Entry]bool
}

func NewAuthoredNativeReplayCatalog(db *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader, bucket string, raw []byte) (SQLAuthoredNativeReplayCatalog, error) {
	r, err := v2input.DecodeAuthoredNativeReplayRequest(append([]byte(nil), raw...))
	if err != nil || db == nil || objects == nil || bucket == "" {
		return SQLAuthoredNativeReplayCatalog{}, v2input.ErrInvalid
	}
	return SQLAuthoredNativeReplayCatalog{Scheduler: db, Objects: objects, Bucket: bucket, request: r, raw: string(append([]byte(nil), raw...))}, nil
}
func (s SQLAuthoredNativeReplayCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	var out objectstore.Object
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if tenant != s.request.Consumer.Tenant || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Scheduler == nil {
		return out, v2input.ErrInvalid
	}
	err := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		var role, bucket string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		if err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.authored_native_replay_input_object($1,$2,$3,$4,$5,$6)`, tenant, s.raw, who.UserID, who.SessionID, node.Ref.ArtifactID, node.Ref.SHA256).Scan(&bucket, &out.Key, &out.VersionID, &out.SHA256, &out.Bytes, &out.ContentType); err != nil {
			return err
		}
		if bucket != s.Bucket || out.Key == "" || out.VersionID == "" || out.VersionID == "null" || out.SHA256 != node.Ref.SHA256 || out.Bytes < 1 {
			return v2input.ErrInvalid
		}
		return nil
	})
	return out, err
}
func (s SQLAuthoredNativeReplayCatalog) read(ctx context.Context, who auth.Identity, tenant uuid.UUID, ref v2input.Ref) ([]byte, error) {
	node := v2draft.LayerRef{Ref: ref}
	before, err := s.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, before, ref)
	if err != nil {
		return nil, err
	}
	after, err := s.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, v2input.ErrConflict
	}
	return raw, nil
}
func (s SQLAuthoredNativeReplayCatalog) Geometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	empty := v2draft.DocumentGeometry{}
	if node.Contract == "MaskRef" {
		return s.plannedMaskGeometry(ctx, who, tenant, node)
	}
	if node.Contract != "WorkingImage" {
		return empty, false, nil
	}
	r := s.request
	if node.Ref == r.Original.Image {
		refs := []v2input.Ref{r.Original.Task.Params, r.NativeSource.Source, r.Original.Image, r.Original.Report}
		raw := make([][]byte, len(refs))
		for i, ref := range refs {
			var err error
			raw[i], err = s.read(ctx, who, tenant, ref)
			if err != nil {
				return empty, false, err
			}
		}
		// This is an adapter to a pure content validator, not a manufactured receipt
		// or adopted-state assertion. SQL independently selected the actual native
		// tuple and physical documents before this call.
		n := r.Original.Task
		phase := AuthoredPhaseReceipt{InputSource: r.NativeSource, Geometry: &AuthoredGeometryPhaseWitness{
			Task: AuthoredPhaseTask{RunID: n.Run, TaskID: n.Task, ResultID: n.Result, Operation: n.Operation, Version: n.Version, Attempt: n.Attempt, Fence: n.Fence, Params: n.Params, InputDigest: n.InputDigest, ProfileJCS: n.ProfileJCS, ProfileSHA: n.ProfileSHA, Outputs: append([]v2input.Ref(nil), n.Outputs...)}, Root: r.Original.Root, Image: r.Original.Image, Report: r.Original.Report}}
		geometry, err := validateAuthoredUpscaleGeometry(phase, raw[0], raw[1], raw[2], raw[3])
		return geometry, err == nil, err
	}
	bindings := []v2input.SessionVersionBinding{r.Consumer.Source, r.NativeSource}
	if r.PhaseReceipt != nil {
		raw, err := s.read(ctx, who, tenant, *r.PhaseReceipt)
		if err != nil {
			return empty, false, err
		}
		var phase AuthoredPhaseReceipt
		if json.Unmarshal(raw, &phase) != nil || phase.Ordinal != r.GeometryOrdinal || phase.Instruction != r.GeometryInstruction || phase.Geometry == nil {
			return empty, false, v2input.ErrInvalid
		}
		// Full native tuple equality; no accepted IDs are transferred to new work.
		expected, _ := canonical.Marshal(r.Original)
		actual, _ := canonical.Marshal(phase.Geometry)
		if !bytes.Equal(expected, actual) {
			return empty, false, v2input.ErrInvalid
		}
		bindings = append(bindings, phase.InputSource, phase.OutputSource)
	}
	for _, binding := range bindings {
		if node.Ref != binding.Source {
			continue
		}
		raw, err := s.read(ctx, who, tenant, node.Ref)
		if err != nil {
			return empty, false, err
		}
		image, err := review.ParseWorkingImage(raw)
		if err != nil {
			return empty, false, err
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil || canvas != binding.CanvasHash {
			return empty, false, v2input.ErrInvalid
		}
		chain, err := canonical.Hash(image.TransformChain)
		if err != nil {
			return empty, false, err
		}
		return v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}, true, nil
	}
	// Exact planned occurrences retain their own geometry; no original-canvas fallback.
	return s.plannedImageGeometry(ctx, who, tenant, node)
}
