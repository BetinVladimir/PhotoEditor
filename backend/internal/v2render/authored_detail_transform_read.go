package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type CurrentAuthoredDetailTransform struct {
	document AuthoredDetailTransformDocument
	proof    json.RawMessage
}

func (p CurrentAuthoredDetailTransform) Document() AuthoredDetailTransformDocument {
	return AuthoredDetailTransformDocument{Root: append([]byte(nil), p.document.Root...), Target: append([]byte(nil), p.document.Target...), TargetRef: p.document.TargetRef}
}
func (p CurrentAuthoredDetailTransform) Proof() json.RawMessage {
	return append(json.RawMessage(nil), p.proof...)
}

// Witness is the exact closed SQL content-authority ABI; a bare producer tuple
// does not carry the physically read canonical root and target documents.
func (p CurrentAuthoredDetailTransform) Witness() (json.RawMessage, error) {
	if !json.Valid(p.proof) || !authoredDetailCanonical(p.document.Root) || !authoredDetailCanonical(p.document.Target) {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(map[string]any{"proof": json.RawMessage(p.proof), "root_jcs": string(p.document.Root), "target_jcs": string(p.document.Target)})
}

type SQLAuthoredDetailTransformReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}
type authoredDetailTransformObject struct {
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"key"`
	Version string      `json:"version"`
	Bytes   int64       `json:"bytes"`
}
type authoredDetailTransformOutput struct {
	authoredDetailTransformObject
	Role      string `json:"role"`
	MediaType string `json:"media_type"`
}
type authoredDetailTransformLocator struct {
	Outputs []authoredDetailTransformOutput `json:"outputs"`
	Proof   json.RawMessage                 `json:"proof"`
	Root    authoredDetailTransformObject   `json:"root"`
	Target  authoredDetailTransformObject   `json:"target"`
}

func (s SQLAuthoredDetailTransformReader) lookup(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, task uuid.UUID, root, target *string) (authoredDetailTransformLocator, bool, error) {
	empty := authoredDetailTransformLocator{}
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	if s.Scheduler == nil || s.Objects == nil || s.Bucket == "" || task == uuid.Nil || source.Tenant == uuid.Nil || source.Parent == uuid.Nil || source.Actor != who.UserID || source.Session != who.SessionID || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, false, v2input.ErrInvalid
	}
	var raw []byte
	err := s.Scheduler.QueryRow(ctx, `SELECT photo.authored_render_layer_mask_transform_objects($1,$2,$3,$4,$5,$6,$7,$8)`, source.Tenant, source.Parent, source.Instruction.Ordinal, who.UserID, who.SessionID, task, root, target).Scan(&raw)
	if err != nil {
		return empty, false, err
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return empty, false, nil
	}
	if len(raw) > 16384 {
		return empty, false, v2input.ErrInvalid
	}
	wire, err := canonical.Bytes(raw)
	if err != nil {
		return empty, false, err
	}
	var out authoredDetailTransformLocator
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return empty, false, v2input.ErrInvalid
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return empty, false, v2input.ErrInvalid
	}
	for _, o := range []authoredDetailTransformObject{out.Root, out.Target} {
		if !validLayerArtifactRef(o.Ref) || o.Bucket != s.Bucket || o.Key == "" || o.Version == "" || o.Version == "null" || o.Bytes < 2 || o.Bytes > 1048576 {
			return empty, false, v2input.ErrInvalid
		}
	}
	var proof struct {
		Tenant  uuid.UUID   `json:"tenant_id"`
		Parent  uuid.UUID   `json:"parent_run_id"`
		Task    uuid.UUID   `json:"task_id"`
		Result  uuid.UUID   `json:"result_id"`
		Attempt int         `json:"attempt"`
		Fence   int64       `json:"fence"`
		Root    v2input.Ref `json:"root"`
		Target  v2input.Ref `json:"target_image"`
	}
	d = json.NewDecoder(bytes.NewReader(out.Proof))
	d.DisallowUnknownFields()
	if d.Decode(&proof) != nil || proof.Tenant != source.Tenant || proof.Parent != source.Parent || proof.Task != task || proof.Result == uuid.Nil || proof.Attempt < 1 || proof.Fence < 1 || proof.Root != out.Root.Ref || proof.Target != out.Target.Ref {
		return empty, false, v2input.ErrInvalid
	}
	return out, true, nil
}

// Read never accepts a caller's output locators or witness bytes. It reads the
// independent producer's exact immutable JSON versions, then repeats both the
// protected producer tuple and SQL content authority before returning witnesses.
func (s SQLAuthoredDetailTransformReader) Read(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, task uuid.UUID) (CurrentAuthoredDetailTransform, bool, error) {
	empty := CurrentAuthoredDetailTransform{}
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	if s.Scheduler == nil {
		return empty, false, v2input.ErrInvalid
	}
	actual, present, err := ReadAuthoredInstructionSource(ctx, s.Scheduler, source.Tenant, source.Parent, source.Instruction.Ordinal)
	if err != nil {
		return empty, false, err
	}
	if !present {
		return empty, false, nil
	}
	expected, err := canonical.Marshal(source)
	if err != nil {
		return empty, false, err
	}
	observed, err := canonical.Marshal(actual)
	if err != nil {
		return empty, false, err
	}
	if !bytes.Equal(expected, observed) {
		return empty, false, v2input.ErrExpired
	}
	first, found, err := s.lookup(ctx, who, source, task, nil, nil)
	if err != nil || !found {
		return empty, found, err
	}
	read := func(o authoredDetailTransformObject) ([]byte, error) {
		return v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objectstore.Object{Key: o.Key, VersionID: o.Version, SHA256: o.Ref.SHA256, Bytes: o.Bytes, ContentType: "application/json"}, o.Ref)
	}
	root, err := read(first.Root)
	if err != nil {
		return empty, false, err
	}
	target, err := read(first.Target)
	if err != nil {
		return empty, false, err
	}
	if err = s.verifyOutputs(ctx, first, root); err != nil {
		return empty, false, err
	}
	rootJCS, targetJCS := string(root), string(target)
	last, found, err := s.lookup(ctx, who, source, task, &rootJCS, &targetJCS)
	if err != nil || !found {
		return empty, false, err
	}
	a, err := canonical.Marshal(first)
	if err != nil {
		return empty, false, err
	}
	b, err := canonical.Marshal(last)
	if err != nil {
		return empty, false, err
	}
	if !bytes.Equal(a, b) {
		return empty, false, v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return empty, false, err
	}
	return CurrentAuthoredDetailTransform{document: AuthoredDetailTransformDocument{Root: root, Target: target, TargetRef: first.Target.Ref}, proof: append(json.RawMessage(nil), first.Proof...)}, true, nil
}

// Fresh physical output closure: canonical adoption metadata alone cannot
// prove the currently served NPY/PNG versions still contain the committed bytes.
func (s SQLAuthoredDetailTransformReader) verifyOutputs(ctx context.Context, locator authoredDetailTransformLocator, root []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(locator.Outputs) != 2 {
		return v2input.ErrInvalid
	}
	mask, err := review.ParseTransformedMask(root)
	if err != nil {
		return err
	}
	seen := map[v2input.Ref]bool{locator.Root.Ref: true, locator.Target.Ref: true}
	for i, o := range locator.Outputs {
		expectedRole, expectedMedia, maxBytes := "transformed_mask", "application/octet-stream", int64(801048576)
		if i == 1 {
			expectedRole = "transformed_mask_png"
			expectedMedia = "image/png"
			maxBytes = 613048576
		}
		if o.Role != expectedRole || o.MediaType != expectedMedia || !validLayerArtifactRef(o.Ref) || seen[o.Ref] || o.Bucket != s.Bucket || o.Key == "" || o.Version == "" || o.Version == "null" || o.Bytes < 1 || o.Bytes > maxBytes {
			return v2input.ErrInvalid
		}
		if i == 0 && (o.Ref.ArtifactID != mask.Artifact.ID || o.Ref.SHA256 != mask.Artifact.SHA256) {
			return v2input.ErrInvalid
		}
		seen[o.Ref] = true
		if err = verifyAuthoredSegmentStream(ctx, s.Objects, objectstore.Object{Key: o.Key, VersionID: o.Version, SHA256: o.Ref.SHA256, Bytes: o.Bytes, ContentType: o.MediaType}, o.Ref); err != nil {
			return err
		}
	}
	return ctx.Err()
}
