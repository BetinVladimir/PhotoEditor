package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type CurrentAuthoredSkin struct {
	observation AuthoredFaceProducerObservation
	mask        review.ProtectionMask
	root        v2input.Ref
	proof       []byte
}
type authoredSkinObject struct {
	ref    v2input.Ref
	object objectstore.Object
}

// Read performs independent physical class1 derivation and PNG replay against
// the actual current parser map. The returned value is private current evidence,
// not a new face decision or a completed/approved parent receipt.
func (p AuthoredSkinPreparation) Read(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, imageRaw []byte, selection CurrentAuthoredFaceSelection) (CurrentAuthoredSkin, error) {
	var zero CurrentAuthoredSkin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Producers.Scheduler != p.Scheduler || p.Producers.Objects == nil || p.Producers.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	if err := RecheckCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, selection); err != nil {
		return zero, err
	}
	if authoredFaceCandidateSHA(imageRaw) != source.Source.Source.SHA256 {
		return zero, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != source.Source.CanvasHash {
		return zero, v2input.ErrInvalid
	}
	read := func() ([]byte, error) {
		var raw []byte
		err := database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT photo.read_authored_render_skin_producer($1,$2,$3,$4)", source.Tenant, source.Parent, source.Instruction.Ordinal, "mask.face_skin").Scan(&raw)
		})
		return raw, err
	}
	before, err := read()
	if err != nil {
		return zero, err
	}
	if len(before) == 0 {
		return zero, v2input.ErrExpired
	}
	var o AuthoredFaceProducerObservation
	if json.Unmarshal(before, &o) != nil || o.Operation != "mask.face_skin" || o.Version != "1" || len(o.Roots) != 3 {
		return zero, v2input.ErrInvalid
	}
	parser, err := p.Producers.read(ctx, source, "face.parse")
	if err != nil {
		return zero, err
	}
	if !authoredSkinProducerEqual(selection.candidate.Parser, parser.observation) {
		return zero, v2input.ErrExpired
	}
	locate := func(op string, ref v2input.Ref, media string) (authoredSkinObject, error) {
		var out authoredSkinObject
		out.ref = ref
		var bucket string
		if !validLayerArtifactRef(ref) {
			return out, v2input.ErrInvalid
		}
		query := "SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.authored_render_skin_producer_object($1,$2,$3,$4,$5,$6)"
		if op == "face.parse" {
			query = "SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.authored_render_face_producer_object($1,$2,$3,$4,$5,$6)"
		}
		err := database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, query, source.Tenant, source.Parent, source.Instruction.Ordinal, op, ref.ArtifactID, ref.SHA256).Scan(&bucket, &out.object.Key, &out.object.VersionID, &out.object.SHA256, &out.object.Bytes, &out.object.ContentType)
		})
		if err != nil {
			return out, err
		}
		if bucket != p.Producers.Bucket || out.object.SHA256 != ref.SHA256 || out.object.ContentType != media || out.object.Bytes < 1 || out.object.Key == "" || out.object.VersionID == "" || out.object.VersionID == "null" {
			return out, v2input.ErrInvalid
		}
		return out, nil
	}
	refs := []v2input.Ref{o.Roots[0], o.Roots[1], o.Roots[2], o.Params, parser.observation.Roots[0]}
	media := []string{"application/octet-stream", "image/png", "application/json", "application/json", "application/octet-stream"}
	pins := make([]authoredSkinObject, len(refs))
	seen := map[string]bool{}
	for i, ref := range refs {
		if seen[ref.ArtifactID.String()] {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID.String()] = true
		op := "mask.face_skin"
		if i == 4 {
			op = "face.parse"
		}
		pins[i], err = locate(op, ref, media[i])
		if err != nil {
			return zero, err
		}
	}
	rootRaw, err := v2input.ReadPinnedSemanticDocument(ctx, p.Producers.Objects, pins[2].object, pins[2].ref)
	if err != nil {
		return zero, err
	}
	canonicalRoot, err := canonical.Bytes(rootRaw)
	if err != nil || !bytes.Equal(rootRaw, canonicalRoot) {
		return zero, v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(rootRaw)
	if err != nil {
		return zero, err
	}
	if mask.Artifact.ID != o.Roots[0].ArtifactID || mask.Artifact.SHA256 != o.Roots[0].SHA256 {
		return zero, v2input.ErrInvalid
	}
	params, err := v2input.ReadPinnedSemanticDocument(ctx, p.Producers.Objects, pins[3].object, pins[3].ref)
	if err != nil {
		return zero, err
	}
	ref := func(r v2input.Ref) review.ArtifactRef { return review.ArtifactRef{ID: r.ArtifactID, SHA256: r.SHA256} }
	labels, err := review.VerifyFaceSkinParams(params, ref(o.Params), parser.root, ref(parser.observation.Roots[1]), review.ProtectionMask(mask), image.Source(ref(source.Source.Source)))
	if err != nil {
		return zero, err
	}
	verifyPair := func(left, right authoredSkinObject, skin bool) (int64, error) {
		a, e := p.Producers.Objects.Open(ctx, left.object)
		if e != nil {
			return 0, e
		}
		b, e := p.Producers.Objects.Open(ctx, right.object)
		if e != nil {
			_ = a.Close()
			return 0, e
		}
		l := masknpy.PinnedMask{Reader: a, Bytes: left.object.Bytes, SHA256: left.ref.SHA256}
		r := masknpy.PinnedMask{Reader: b, Bytes: right.object.Bytes, SHA256: right.ref.SHA256}
		var count int64
		if skin {
			count, e = masknpy.VerifyFaceSkinNPY(ctx, l, r, image.Canvas.Width, image.Canvas.Height)
		} else {
			e = masknpy.VerifyProbabilityMaskPNG(ctx, l, r, image.Canvas.Width, image.Canvas.Height)
		}
		ae, be := a.Close(), b.Close()
		if e != nil {
			return 0, e
		}
		if ae != nil {
			return 0, ae
		}
		if be != nil {
			return 0, be
		}
		return count, nil
	}
	count, err := verifyPair(pins[4], pins[0], true)
	if err != nil {
		return zero, err
	}
	if err = review.VerifyFaceSkinConfidence(review.ProtectionMask(mask), labels, count); err != nil {
		return zero, err
	}
	if _, err = verifyPair(pins[0], pins[1], false); err != nil {
		return zero, err
	}
	required := mask.Confidence < .75
	if o.ReviewRequired != required || (required && o.RunState != "waiting_review") || (!required && o.RunState != "completed") {
		return zero, v2input.ErrInvalid
	}
	after, err := read()
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(before, after) {
		return zero, v2input.ErrExpired
	}
	for i, pin := range pins {
		op := "mask.face_skin"
		if i == 4 {
			op = "face.parse"
		}
		current, e := locate(op, pin.ref, media[i])
		if e != nil {
			return zero, e
		}
		if current != pin {
			return zero, v2input.ErrExpired
		}
	}
	if err = RecheckCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, selection); err != nil {
		return zero, err
	}
	return CurrentAuthoredSkin{observation: o, mask: review.ProtectionMask(mask), root: o.Roots[2], proof: append([]byte(nil), after...)}, ctx.Err()
}
