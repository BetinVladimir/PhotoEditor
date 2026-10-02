package v2region

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type RegionObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}
type checkedRegionReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r checkedRegionReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// readRegionDocument accepts only exact version-pinned, bounded JSON metadata.
// The metadata's scoped authority must be established independently by SQL.
func readRegionDocument(ctx context.Context, objects RegionObjectReader, object objectstore.Object, ref v2input.Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if objects == nil || ref.ArtifactID == [16]byte{} || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > 1048576 || object.SHA256 != ref.SHA256 {
		return nil, ErrRegionInvalid
	}
	stream, err := objects.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, ErrRegionInvalid
	}
	body, readErr := io.ReadAll(io.LimitReader(checkedRegionReader{ctx, stream}, object.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != object.Bytes || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, ErrRegionInvalid
	}
	return body, nil
}

func LoadRegionEvidence(ctx context.Context, objects RegionObjectReader, req RegionPreviewRequest, recipeObject objectstore.Object, sourceRef v2input.Ref, sourceObject objectstore.Object) (CompiledRegionEvidence, error) {
	var empty CompiledRegionEvidence
	recipe, err := readRegionDocument(ctx, objects, recipeObject, v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256})
	if err != nil {
		return empty, err
	}
	source, err := readRegionDocument(ctx, objects, sourceObject, sourceRef)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	result, err := CompileRegionEvidence(req, recipe, sourceRef, source)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
