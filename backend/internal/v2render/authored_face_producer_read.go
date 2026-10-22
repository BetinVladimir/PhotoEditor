package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type AuthoredFaceProducerReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}
type currentAuthoredFaceProducer struct {
	observation  AuthoredFaceProducerObservation
	root, params []byte
	proof        []byte
}

// Read only the actual current canonical producer. Exact metadata is read again
// after original-version physical reads. This immutable observation retains its
// warning review bit; it is not a selection/phase-completion authority.
func (r AuthoredFaceProducerReader) read(ctx context.Context, source AuthoredInstructionSource, op string) (currentAuthoredFaceProducer, error) {
	var out currentAuthoredFaceProducer
	if r.Scheduler == nil || r.Objects == nil || r.Bucket == "" {
		return out, v2input.ErrInvalid
	}
	read := func() ([]byte, error) {
		var raw []byte
		err := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT photo.read_authored_render_face_producer($1,$2,$3,$4)", source.Tenant, source.Parent, source.Instruction.Ordinal, op).Scan(&raw)
		})
		return raw, err
	}
	before, err := read()
	if err != nil {
		return out, err
	}
	if len(before) == 0 {
		return out, v2input.ErrExpired
	}
	if json.Unmarshal(before, &out.observation) != nil || out.observation.Operation != op || out.observation.Version != "1" {
		return out, v2input.ErrInvalid
	}
	rootRef, err := authoredFaceProducerRoot(op, out.observation)
	if err != nil {
		return out, err
	}
	root, rootObject, err := r.document(ctx, source, op, rootRef)
	if err != nil {
		return out, err
	}
	if err := validateAuthoredFaceProducerDocument(source, op, out.observation, root); err != nil {
		return out, err
	}
	params, paramsObject, err := r.document(ctx, source, op, out.observation.Params)
	if err != nil {
		return out, err
	}
	after, err := read()
	if err != nil {
		return out, err
	}
	if !bytes.Equal(before, after) {
		return out, v2input.ErrExpired
	}
	// Each locator is also reacquired, so a matching observation cannot hide a
	// changed object-version locator during the physical IO interval.
	currentRoot, err := r.locator(ctx, source, op, rootRef)
	if err != nil {
		return out, err
	}
	currentParams, err := r.locator(ctx, source, op, out.observation.Params)
	if err != nil {
		return out, err
	}
	if currentRoot != rootObject || currentParams != paramsObject {
		return out, v2input.ErrExpired
	}
	out.root = root
	out.params = params
	out.proof = append([]byte(nil), after...)
	return out, ctx.Err()
}
func (r AuthoredFaceProducerReader) locator(ctx context.Context, source AuthoredInstructionSource, op string, ref v2input.Ref) (objectstore.Object, error) {
	var obj objectstore.Object
	var bucket, media string
	err := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.authored_render_face_producer_object($1,$2,$3,$4,$5,$6)", source.Tenant, source.Parent, source.Instruction.Ordinal, op, ref.ArtifactID, ref.SHA256).Scan(&bucket, &obj.Key, &obj.VersionID, &obj.SHA256, &obj.Bytes, &media)
	})
	if err != nil {
		return obj, err
	}
	if bucket != r.Bucket || obj.SHA256 != ref.SHA256 || obj.Bytes < 2 || obj.Bytes > 1<<20 || media != "application/json" || obj.VersionID == "" || obj.VersionID == "null" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	obj.ContentType = media
	return obj, ctx.Err()
}
func (r AuthoredFaceProducerReader) document(ctx context.Context, source AuthoredInstructionSource, op string, ref v2input.Ref) ([]byte, objectstore.Object, error) {
	obj, err := r.locator(ctx, source, op, ref)
	if err != nil {
		return nil, obj, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, obj, ref)
	return raw, obj, err
}

// Parser emits its uint16 labels first and canonical FaceLabels19 root last.
// Model inference roots remain exact operation-specific ordered vectors.
func authoredFaceProducerRoot(op string, observation AuthoredFaceProducerObservation) (v2input.Ref, error) {
	count := 1
	switch op {
	case "face.detect", "face.landmarks":
	case "face.parse":
		count = 2
	default:
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if observation.Operation != op || observation.Version != "1" || len(observation.Roots) != count {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	seen := map[string]bool{}
	for _, ref := range observation.Roots {
		if !validLayerArtifactRef(ref) || seen[ref.ArtifactID.String()] {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		seen[ref.ArtifactID.String()] = true
	}
	return observation.Roots[count-1], nil
}

// Validate the selected physical canonical root before returning it. Full model,
// source geometry and detector inventory verification remains in the candidate
// compiler; this boundary additionally binds the parser's separate map output.
func validateAuthoredFaceProducerDocument(source AuthoredInstructionSource, op string, observation AuthoredFaceProducerObservation, raw []byte) error {
	ref, err := authoredFaceProducerRoot(op, observation)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	c, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(c, raw) || len(raw) < 2 || len(raw) > 1<<20 || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	if op == "face.parse" {
		var labels review.FaceLabels19
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&labels) != nil || labels.Image.ID != source.Source.Source.ArtifactID || labels.Image.SHA256 != source.Source.Source.SHA256 || labels.Labels.ID != observation.Roots[0].ArtifactID || labels.Labels.SHA256 != observation.Roots[0].SHA256 {
			return v2input.ErrInvalid
		}
		canvas, e := canonical.Hash(labels.Canvas)
		if e != nil || canvas != source.Source.CanvasHash {
			return v2input.ErrInvalid
		}
	}
	return nil
}
