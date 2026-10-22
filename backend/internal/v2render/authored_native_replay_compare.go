package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type authoredNativeByteEquality struct {
	Role     string      `json:"role"`
	Original v2input.Ref `json:"original"`
	Replay   v2input.Ref `json:"replay"`
	SHA      string      `json:"sha256"`
	Bytes    int64       `json:"bytes"`
}
type authoredNativeEvidence struct {
	Schema      int                               `json:"schema_version"`
	Request     v2input.Ref                       `json:"request"`
	RequestSHA  string                            `json:"request_sha256"`
	Replay      v2input.AuthoredNativeObservation `json:"replay"`
	Comparisons []authoredNativeByteEquality      `json:"comparisons"`
}
type authoredNativeComparisonObject struct {
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"object_key"`
	Version string      `json:"object_version"`
	Bytes   int64       `json:"bytes"`
	Media   string      `json:"media_type"`
}

func (o authoredNativeComparisonObject) object() objectstore.Object {
	return objectstore.Object{Key: o.Key, VersionID: o.Version, SHA256: o.Ref.SHA256, Bytes: o.Bytes, ContentType: o.Media}
}
func (p AuthoredNativeReplayPublication) comparisonScope(ctx context.Context, tenant, task uuid.UUID, body []byte) ([]byte, []authoredNativeComparisonObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var raw []byte
	err := p.transaction(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.read_authored_native_replay_comparison($1,$2,$3)`, tenant, task, string(body)).Scan(&raw)
	})
	if err != nil {
		return nil, nil, err
	}
	var row struct {
		Scope   json.RawMessage                  `json:"scope"`
		Objects []authoredNativeComparisonObject `json:"objects"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) < 2 || d.Decode(&row) != nil || len(row.Scope) < 2 || len(row.Objects) < 1 || len(row.Objects) > 260 {
		return nil, nil, v2input.ErrInvalid
	}
	owned, err := canonical.Bytes(raw)
	if err != nil {
		return nil, nil, err
	}
	seen := map[uuid.UUID]bool{}
	for _, o := range row.Objects {
		if o.Bucket != p.Bucket || o.Key == "" || o.Version == "" || o.Version == "null" || o.Bytes < 1 || o.Bytes > 9007199254740991 || seen[o.Ref.ArtifactID] {
			return nil, nil, v2input.ErrInvalid
		}
		seen[o.Ref.ArtifactID] = true
	}
	return owned, row.Objects, nil
}

// verifyEvidence performs the concrete physical proof BEFORE reserve/publication.
// SQL independently selects the actual old/new canonical tuples and current
// qualification. Re-reading the entire scoped row after IO detects withdrawal.
// No callback may nominate accepted bytes, geometry, transport or producer IDs.
func (p AuthoredNativeReplayPublication) verifyEvidence(ctx context.Context, tenant, task uuid.UUID, requestRaw, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := v2input.DecodeAuthoredNativeReplayRequest(requestRaw)
	if err != nil {
		return err
	}
	var e authoredNativeEvidence
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil {
		return v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(e)
	if err != nil || !bytes.Equal(exact, body) || e.Schema != 1 {
		return v2input.ErrInvalid
	}
	before, objects, err := p.comparisonScope(ctx, tenant, task, body)
	if err != nil {
		return err
	}
	byID := map[uuid.UUID]authoredNativeComparisonObject{}
	for _, o := range objects {
		byID[o.Ref.ArtifactID] = o
	}
	object := func(ref v2input.Ref) (objectstore.Object, error) {
		o, ok := byID[ref.ArtifactID]
		if !ok || o.Ref != ref {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		return o.object(), nil
	}
	read := func(ref v2input.Ref) ([]byte, error) {
		o, err := object(ref)
		if err != nil {
			return nil, err
		}
		return v2input.ReadPinnedSemanticDocument(ctx, p.Objects, o, ref)
	}
	params, err := read(r.Original.Task.Params)
	if err != nil {
		return err
	}
	source, err := read(r.NativeSource.Source)
	if err != nil {
		return err
	}
	images := make([]review.WorkingImage, 2)
	reports := make([][]byte, 2)
	for i, n := range []v2input.AuthoredNativeObservation{r.Original, e.Replay} {
		imageRaw, err := read(n.Image)
		if err != nil {
			return err
		}
		reports[i], err = read(n.Report)
		if err != nil {
			return err
		}
		producer := n.Task
		phase := AuthoredPhaseReceipt{InputSource: r.NativeSource, Geometry: &AuthoredGeometryPhaseWitness{Task: AuthoredPhaseTask{RunID: producer.Run, TaskID: producer.Task, ResultID: producer.Result, Operation: producer.Operation, Version: producer.Version, Attempt: producer.Attempt, Fence: producer.Fence, Params: producer.Params, InputDigest: producer.InputDigest, ProfileJCS: producer.ProfileJCS, ProfileSHA: producer.ProfileSHA, Outputs: append([]v2input.Ref(nil), producer.Outputs...)}, Root: n.Root, Image: n.Image, Report: n.Report}}
		if _, err := validateAuthoredUpscaleGeometry(phase, params, source, imageRaw, reports[i]); err != nil {
			return err
		}
		images[i], err = review.ParseWorkingImage(imageRaw)
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(reports[0], reports[1]) || images[0].Canvas != images[1].Canvas || images[0].Color != images[1].Color || images[0].DType != images[1].DType || !bytes.Equal(images[0].TransformChain, images[1].TransformChain) || (images[0].Alpha == nil) != (images[1].Alpha == nil) {
		return v2input.ErrInvalid
	}
	expected := []authoredNativeByteEquality{{Role: "pixels", Original: v2input.Ref{ArtifactID: images[0].Pixels.ID, SHA256: images[0].Pixels.SHA256}, Replay: v2input.Ref{ArtifactID: images[1].Pixels.ID, SHA256: images[1].Pixels.SHA256}}}
	if images[0].Alpha != nil {
		expected = append(expected, authoredNativeByteEquality{Role: "alpha", Original: v2input.Ref{ArtifactID: images[0].Alpha.ID, SHA256: images[0].Alpha.SHA256}, Replay: v2input.Ref{ArtifactID: images[1].Alpha.ID, SHA256: images[1].Alpha.SHA256}})
	}
	if len(e.Comparisons) != len(expected) {
		return v2input.ErrInvalid
	}
	// Match the complete native output closure, not merely a subset whose
	// pixels happen to compare. No extra opaque output can evade verification.
	allowed := map[v2input.Ref]bool{e.Replay.Root: true, e.Replay.Report: true}
	for _, pair := range expected {
		allowed[pair.Replay] = true
	}
	if len(allowed) != len(expected)+2 || len(e.Replay.Task.Outputs) != len(allowed) {
		return v2input.ErrInvalid
	}
	for _, ref := range e.Replay.Task.Outputs {
		if !allowed[ref] {
			return v2input.ErrInvalid
		}
	}
	for i, pair := range expected {
		c := e.Comparisons[i]
		if c.Role != pair.Role || c.Original != pair.Original || c.Replay != pair.Replay || c.Original.ArtifactID == c.Replay.ArtifactID {
			return v2input.ErrInvalid
		}
		left, err := object(c.Original)
		if err != nil {
			return err
		}
		right, err := object(c.Replay)
		if err != nil {
			return err
		}
		if c.SHA != c.Original.SHA256 || c.SHA != c.Replay.SHA256 || c.Bytes != left.Bytes || c.Bytes != right.Bytes {
			return v2input.ErrInvalid
		}
		if err := p.compareStreams(ctx, left, right); err != nil {
			return err
		}
	}
	after, _, err := p.comparisonScope(ctx, tenant, task, body)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
func (p AuthoredNativeReplayPublication) compareStreams(ctx context.Context, left, right objectstore.Object) (err error) {
	if p.Objects == nil || left.Bytes < 1 || left.Bytes != right.Bytes || left.Bytes > 9007199254740991 || left.SHA256 != right.SHA256 {
		return v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	a, err := p.Objects.Open(ctx, left)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := a.Close(); err == nil {
			err = closeErr
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	b, err := p.Objects.Open(ctx, right)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := b.Close(); err == nil {
			err = closeErr
		}
	}()
	ah, bh := sha256.New(), sha256.New()
	remaining := left.Bytes
	for remaining > 0 {
		if err = ctx.Err(); err != nil {
			return err
		}
		n := int64(65536)
		if remaining < n {
			n = remaining
		}
		x, y := make([]byte, n), make([]byte, n)
		if _, err = io.ReadFull(a, x); err != nil {
			return err
		}
		if _, err = io.ReadFull(b, y); err != nil {
			return err
		}
		if !bytes.Equal(x, y) {
			return v2input.ErrConflict
		}
		ah.Write(x)
		bh.Write(y)
		remaining -= n
	}
	for _, reader := range []io.Reader{a, b} {
		var extra [1]byte
		n, e := reader.Read(extra[:])
		if n != 0 || e != io.EOF {
			return v2input.ErrInvalid
		}
	}
	if hex.EncodeToString(ah.Sum(nil)) != left.SHA256 || hex.EncodeToString(bh.Sum(nil)) != right.SHA256 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
