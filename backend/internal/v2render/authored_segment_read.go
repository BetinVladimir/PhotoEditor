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
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// CurrentAuthoredSegment can only be constructed after current SQL canonical
// adoption and physical typed document reads. It is not user review approval.
type CurrentAuthoredSegment struct {
	task           AuthoredPhaseTask
	result, report v2input.Ref
	output         v2input.SessionVersionBinding
	closure        []v2input.Ref
	proof          []byte
}
type AuthoredSegmentProducerReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}
type authoredSegmentObject struct {
	Ref    v2input.Ref
	Object objectstore.Object
	Role   string
}

func (r AuthoredSegmentProducerReader) Read(ctx context.Context, source AuthoredInstructionSource) (CurrentAuthoredSegment, error) {
	zero := CurrentAuthoredSegment{}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r.Scheduler == nil || r.Objects == nil || r.Bucket == "" || source.Instruction.Kind != "segment" {
		return zero, v2input.ErrInvalid
	}
	read := func() ([]byte, error) {
		var raw []byte
		err := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT photo.read_authored_render_segment_producer($1,$2,$3)`, source.Tenant, source.Parent, source.Instruction.Ordinal).Scan(&raw)
		})
		return raw, err
	}
	before, err := read()
	if err != nil {
		return zero, err
	}
	if len(before) == 0 {
		return zero, ErrAuthoredSegmentPending
	}
	var observation struct {
		AuthoredPhaseTask
		Profile  json.RawMessage `json:"profile"`
		RunState string          `json:"run_state"`
		Review   bool            `json:"review_required"`
	}
	if json.Unmarshal(before, &observation) != nil || observation.Operation != "render.authored_segment" || observation.Version != "1" || !observation.Review || len(observation.Outputs) < 1 || len(observation.Outputs) > 128 {
		return zero, v2input.ErrInvalid
	}
	profile, err := canonical.Bytes(observation.Profile)
	if err != nil {
		return zero, err
	}
	h := sha256.Sum256(profile)
	observation.ProfileJCS = string(profile)
	observation.ProfileSHA = hex.EncodeToString(h[:])
	locate := func(ref v2input.Ref) (authoredSegmentObject, error) {
		out := authoredSegmentObject{Ref: ref}
		var bucket string
		err := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type,output_role FROM photo.authored_render_segment_producer_object($1,$2,$3,$4,$5)`, source.Tenant, source.Parent, source.Instruction.Ordinal, ref.ArtifactID, ref.SHA256).Scan(&bucket, &out.Object.Key, &out.Object.VersionID, &out.Object.SHA256, &out.Object.Bytes, &out.Object.ContentType, &out.Role)
		})
		if err != nil {
			return out, err
		}
		if bucket != r.Bucket || out.Object.SHA256 != ref.SHA256 || out.Object.Key == "" || out.Object.VersionID == "" || out.Object.VersionID == "null" || out.Object.Bytes < 1 {
			return out, v2input.ErrInvalid
		}
		return out, ctx.Err()
	}
	pins := make([]authoredSegmentObject, 0, len(observation.Outputs))
	documents := map[v2input.Ref][]byte{}
	roles := map[v2input.Ref]string{}
	seen := map[v2input.Ref]bool{}
	var result v2input.Ref
	for _, ref := range observation.Outputs {
		if seen[ref] || !validLayerArtifactRef(ref) {
			return zero, v2input.ErrInvalid
		}
		seen[ref] = true
		pin, err := locate(ref)
		if err != nil {
			return zero, err
		}
		pins = append(pins, pin)
		roles[ref] = pin.Role
		maxBytes := int64(8388608)
		media := "application/json"
		switch pin.Role {
		case "document", "working_image", "report", "mask":
		case "pixels":
			media = "application/x-npy"
			maxBytes = 2400004096
		case "alpha", "mask_pixels":
			media = "application/x-npy"
			maxBytes = 800004096
		default:
			return zero, v2input.ErrInvalid
		}
		if pin.Object.ContentType != media || pin.Object.Bytes > maxBytes {
			return zero, v2input.ErrInvalid
		}
		if pin.Object.ContentType == "application/json" {
			raw, err := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, pin.Object, ref)
			if err != nil {
				return zero, err
			}
			wire, err := canonical.Bytes(raw)
			if err != nil || !bytes.Equal(wire, raw) {
				return zero, v2input.ErrInvalid
			}
			documents[ref] = raw
		} else if err := verifyAuthoredSegmentStream(ctx, r.Objects, pin.Object, ref); err != nil {
			return zero, err
		}
		if pin.Role == "document" {
			if result.ArtifactID != ([16]byte{}) {
				return zero, v2input.ErrInvalid
			}
			result = ref
		}
	}
	parsed, err := v2draft.ParseImageResult(documents[result], result)
	if err != nil {
		return zero, err
	}
	if roles[parsed.WorkingImage] != "working_image" || roles[parsed.Report] != "report" || roles[parsed.ChangedMask] != "mask" {
		return zero, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(documents[parsed.WorkingImage])
	if err != nil {
		return zero, err
	}
	refFromReview := func(ref review.ArtifactRef) v2input.Ref { return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256} }
	if roles[refFromReview(image.Pixels)] != "pixels" {
		return zero, v2input.ErrInvalid
	}
	if image.Alpha != nil && roles[refFromReview(*image.Alpha)] != "alpha" {
		return zero, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != source.Source.CanvasHash {
		return zero, v2input.ErrInvalid
	}
	if _, err = v2draft.DocumentChildren(documents[parsed.Report], "ImageReport", parsed.Report); err != nil {
		return zero, err
	}
	var report struct {
		Status    string                          `json:"status"`
		Operation string                          `json:"operation"`
		Sources   []v2input.SessionVersionBinding `json:"sources"`
	}
	if json.Unmarshal(documents[parsed.Report], &report) != nil || report.Status != "review" || report.Operation != "render.authored_segment" || len(report.Sources) != 1 || report.Sources[0] != source.Source {
		return zero, v2input.ErrInvalid
	}
	for _, item := range []struct {
		Ref      v2input.Ref
		Selector string
	}{{parsed.ChangedMask, "changed"}} {
		mask, err := review.ParseMaskRef(documents[item.Ref])
		if err != nil || mask.Selector != item.Selector || mask.Protected || mask.Confidence != 1 || mask.ObjectID != nil || mask.ModelLockID != nil || roles[refFromReview(mask.Artifact)] != "mask_pixels" {
			return zero, v2input.ErrInvalid
		}
		mh, err := canonical.Hash(mask.Canvas)
		if err != nil || mh != canvas {
			return zero, v2input.ErrInvalid
		}
		a, err := canonical.Marshal(mask.TransformChain)
		if err != nil {
			return zero, err
		}
		b, err := canonical.Marshal(image.TransformChain)
		if err != nil || !bytes.Equal(a, b) {
			return zero, v2input.ErrInvalid
		}
	}
	if parsed.GeneratedMask != nil {
		if roles[*parsed.GeneratedMask] != "mask" {
			return zero, v2input.ErrInvalid
		}
		mask, err := review.ParseMaskRef(documents[*parsed.GeneratedMask])
		if err != nil || mask.Selector != "generated" || mask.Protected || mask.Confidence != 1 || mask.ObjectID != nil || mask.ModelLockID != nil || roles[refFromReview(mask.Artifact)] != "mask_pixels" {
			return zero, v2input.ErrInvalid
		}
		mh, err := canonical.Hash(mask.Canvas)
		if err != nil || mh != canvas {
			return zero, v2input.ErrInvalid
		}
		a, err := canonical.Marshal(mask.TransformChain)
		if err != nil {
			return zero, err
		}
		b, err := canonical.Marshal(image.TransformChain)
		if err != nil || !bytes.Equal(a, b) {
			return zero, v2input.ErrInvalid
		}
	}
	expected := map[v2input.Ref]bool{result: true, parsed.WorkingImage: true, parsed.Report: true, parsed.ChangedMask: true, refFromReview(image.Pixels): true}
	if image.Alpha != nil {
		expected[refFromReview(*image.Alpha)] = true
	}
	for _, ref := range []v2input.Ref{parsed.ChangedMask} {
		mask, err := review.ParseMaskRef(documents[ref])
		if err != nil {
			return zero, err
		}
		expected[refFromReview(mask.Artifact)] = true
	}
	if parsed.GeneratedMask != nil {
		expected[*parsed.GeneratedMask] = true
		mask, err := review.ParseMaskRef(documents[*parsed.GeneratedMask])
		if err != nil {
			return zero, err
		}
		expected[refFromReview(mask.Artifact)] = true
	}
	if len(expected) != len(seen) {
		return zero, v2input.ErrInvalid
	}
	for ref := range seen {
		if !expected[ref] {
			return zero, v2input.ErrInvalid
		}
	}
	after, err := read()
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(before, after) {
		return zero, v2input.ErrExpired
	}
	for _, pin := range pins {
		current, err := locate(pin.Ref)
		if err != nil {
			return zero, err
		}
		if current != pin {
			return zero, v2input.ErrExpired
		}
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return CurrentAuthoredSegment{task: observation.AuthoredPhaseTask, result: result, report: parsed.Report, output: v2input.SessionVersionBinding{AssetVersionID: source.Source.AssetVersionID, Source: parsed.WorkingImage, CanvasHash: canvas}, closure: append([]v2input.Ref(nil), observation.Outputs...), proof: append([]byte(nil), after...)}, nil
}
