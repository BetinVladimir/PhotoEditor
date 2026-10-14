package interchange

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type ImportWarpOccurrence struct {
	Original v2input.Ref   `json:"original"`
	From     review.Canvas `json:"from_canvas"`
	To       review.Canvas `json:"to_canvas"`
}
type ImportPreparedScope struct {
	Tenant, Run, Workflow, Entry uuid.UUID
	Binding                      v2input.SessionVersionBinding
	Contract                     string
	Warp                         *ImportWarpOccurrence
	LUT                          *ImportLUTOccurrence
	DefectLabel                  *ImportDefectLabelOccurrence
	Background                   *ImportBackgroundSource
}

// Store commits quota/fenced reservation before IO and rechecks current import
// source, ACL, parsed origin and exact prepared identity when completing.
type ImportPreparedArtifactStore interface {
	ReservePreparedImport(context.Context, ImportPreparedScope, v2input.Ref, int64, string) (artifactjson.Reservation, error)
	CompletePreparedImport(context.Context, ImportPreparedScope, v2input.Ref, artifactjson.Reservation, objectstore.Object) (v2input.Ref, error)
}
type PreparedImportArtifactPublisher struct {
	Scope         ImportPreparedScope
	Store         ImportPreparedArtifactStore
	Objects       artifactjson.Objects
	SourceObjects ImportDocumentObjects
}

func (p PreparedImportArtifactPublisher) scope(entry uuid.UUID, kind string) (ImportPreparedScope, error) {
	s := p.Scope
	s.Contract = kind
	if p.Store == nil || p.Objects == nil || s.Tenant == uuid.Nil || s.Run == uuid.Nil || s.Workflow == uuid.Nil || s.Entry == uuid.Nil || s.Entry != entry || s.Binding.AssetVersionID == uuid.Nil || !validImportRef(s.Binding.Source) || !validImportRef(v2input.Ref{ArtifactID: s.Entry, SHA256: s.Binding.CanvasHash}) {
		return s, v2input.ErrInvalid
	}
	return s, nil
}
func (p PreparedImportArtifactPublisher) PublishPreparedImportRecipe(ctx context.Context, entry uuid.UUID, ref v2input.Ref, raw []byte) (v2input.Ref, error) {
	if err := v2input.ValidateEditingRecipeV3(raw, p.Scope.Binding); err != nil {
		return v2input.Ref{}, err
	}
	if ref.ArtifactID != uuid.NewSHA1(p.Scope.Workflow, []byte("interchange:recipe:"+entry.String())) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	return p.PublishImportDependency(ctx, entry, "EditingRecipeV3", ref, raw)
}
func (p PreparedImportArtifactPublisher) PublishImportDependency(ctx context.Context, entry uuid.UUID, kind string, ref v2input.Ref, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	switch kind {
	case "EditingRecipeV3", "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates":
	default:
		return v2input.Ref{}, v2input.ErrInvalid
	}
	s, err := p.scope(entry, kind)
	if err != nil {
		return v2input.Ref{}, err
	}
	body := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) || len(body) < 2 || len(body) > 8<<20 || body[0] != '{' || !validImportRef(ref) || rawSHA(body) != ref.SHA256 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	return p.publish(ctx, s, ref, int64(len(body)), "application/json", func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil })
}
func (p PreparedImportArtifactPublisher) PublishImportBinary(ctx context.Context, entry uuid.UUID, kind string, ref v2input.Ref, source objectstore.Object) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if kind != "MaskPixels" && kind != "DenseWarpField" && kind != "AbsoluteLUTArray" && kind != "DefectLabelMap" {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	s, err := p.scope(entry, kind)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := validateImportLUTOccurrence(s, ref); err != nil {
		return v2input.Ref{}, err
	}
	if err := validateImportDefectLabelOccurrence(s, ref); err != nil {
		return v2input.Ref{}, err
	}
	if kind == "AbsoluteLUTArray" && s.LUT == nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if kind == "DefectLabelMap" && s.DefectLabel == nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if p.SourceObjects == nil || !validImportRef(ref) || source.Key == "" || source.VersionID == "" || source.VersionID == "null" || source.SHA256 != ref.SHA256 || source.Bytes < 1 || source.Bytes > 128<<30 || (source.ContentType != "application/octet-stream" && source.ContentType != "application/x-npy") {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	return p.publish(ctx, s, ref, source.Bytes, "application/octet-stream", func() (io.ReadCloser, error) { return p.SourceObjects.Open(ctx, source) })
}
func (p PreparedImportArtifactPublisher) PublishImportDefectLabel(ctx context.Context, entry uuid.UUID, ref v2input.Ref, source objectstore.Object, original, parent v2input.Ref, width, height int64) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	scope, err := p.scope(entry, "DefectLabelMap")
	if err != nil {
		return v2input.Ref{}, err
	}
	scope.DefectLabel = &ImportDefectLabelOccurrence{Original: original, Parent: parent, Width: width, Height: height}
	if err := validateImportDefectLabelOccurrence(scope, ref); err != nil {
		return v2input.Ref{}, err
	}
	p.Scope = scope
	return p.PublishImportBinary(ctx, entry, "DefectLabelMap", ref, source)
}
func (p PreparedImportArtifactPublisher) PublishImportLUT(ctx context.Context, entry uuid.UUID, ref v2input.Ref, source objectstore.Object, original, parent v2input.Ref, size int) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	scope, err := p.scope(entry, "AbsoluteLUTArray")
	if err != nil {
		return v2input.Ref{}, err
	}
	scope.LUT = &ImportLUTOccurrence{Original: original, Parent: parent, Size: size}
	if err := validateImportLUTOccurrence(scope, ref); err != nil {
		return v2input.Ref{}, err
	}
	p.Scope = scope
	return p.PublishImportBinary(ctx, entry, "AbsoluteLUTArray", ref, source)
}

// Occurrence publication preserves the exact geometry used by physical NPY
// verification. SQL must independently validate this certificate and identity.
func (p PreparedImportArtifactPublisher) PublishImportWarp(ctx context.Context, entry uuid.UUID, ref v2input.Ref, source objectstore.Object, original v2input.Ref, from, to review.Canvas) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	scope, err := p.scope(entry, "DenseWarpField")
	if err != nil {
		return v2input.Ref{}, err
	}
	scope.Warp = &ImportWarpOccurrence{Original: original, From: from, To: to}
	if err := validateImportWarpOccurrence(scope, ref); err != nil {
		return v2input.Ref{}, err
	}
	p.Scope = scope
	return p.PublishImportBinary(ctx, entry, "DenseWarpField", ref, source)
}

type importPublicationReader struct {
	ctx    context.Context
	source io.Reader
	count  int64
}

func (r *importPublicationReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.source.Read(buffer)
	r.count += int64(n)
	return n, err
}
func (p PreparedImportArtifactPublisher) publish(ctx context.Context, s ImportPreparedScope, ref v2input.Ref, size int64, media string, open func() (io.ReadCloser, error)) (v2input.Ref, error) {
	reservation, err := p.Store.ReservePreparedImport(ctx, s, ref, size, media)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if reservation.ArtifactID != ref.ArtifactID || reservation.Token == uuid.Nil {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	if reservation.Ready != nil {
		if reservation.Ready.ID != ref.ArtifactID || reservation.Ready.SHA256 != ref.SHA256 {
			return v2input.Ref{}, objectstore.ErrIntegrity
		}
		return ref, nil
	}
	key, err := objectstore.DocumentKey(s.Tenant, s.Run, ref.ArtifactID)
	if err != nil {
		return v2input.Ref{}, err
	}
	reader, err := open()
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return v2input.Ref{}, err
	}
	if reader == nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	hash := sha256.New()
	stream := &importPublicationReader{ctx: ctx, source: io.TeeReader(io.LimitReader(reader, size+1), hash)}
	object, putErr := p.Objects.PutImmutable(ctx, key, stream, size, ref.SHA256, media)
	closeErr := reader.Close()
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if closeErr != nil {
		return v2input.Ref{}, closeErr
	}
	if errors.Is(putErr, objectstore.ErrExists) || errors.Is(putErr, objectstore.ErrUnavailable) {
		object, err = p.Objects.RecoverImmutable(ctx, key, size, ref.SHA256, media)
		if err != nil {
			return v2input.Ref{}, err
		}
	} else {
		if putErr != nil {
			return v2input.Ref{}, putErr
		}
		if stream.count != size || hex.EncodeToString(hash.Sum(nil)) != ref.SHA256 {
			return v2input.Ref{}, objectstore.ErrIntegrity
		}
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if object.Key != key || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != ref.SHA256 || object.Bytes != size || object.ContentType != media {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	result, err := p.Store.CompletePreparedImport(ctx, s, ref, reservation, object)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if result != ref {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	return result, nil
}
