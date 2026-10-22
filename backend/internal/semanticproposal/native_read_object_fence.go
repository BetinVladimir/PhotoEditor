package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"

	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Re-read each unique immutable object before a pure-read proof escapes. Locator
// equality alone does not detect storage corruption under an unchanged version.
// The original typed verifier owns format/resource validation; this fence owns
// exact byte identity and uses bounded streaming memory rather than a body cache.
func verifyNativeReadObject(ctx context.Context, reader v2input.SessionAnalysisObjectReader, obj objectstore.Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType == "" || obj.Bytes < 1 || obj.Bytes == math.MaxInt64 {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(obj.SHA256); err != nil {
		return v2input.ErrInvalid
	}
	stream, openErr := reader.Open(ctx, obj)
	if openErr != nil {
		var closeErr error
		if stream != nil {
			closeErr = stream.Close()
		}
		return errors.Join(openErr, closeErr, ctx.Err())
	}
	if stream == nil {
		return v2input.ErrInvalid
	}
	digest := sha256.New()
	n, readErr := io.Copy(digest, nativeReadFenceStream{ctx: ctx, reader: io.LimitReader(stream, obj.Bytes+1)})
	closeErr := stream.Close()
	if err := errors.Join(readErr, closeErr, ctx.Err()); err != nil {
		return err
	}
	if n != obj.Bytes || hex.EncodeToString(digest.Sum(nil)) != obj.SHA256 {
		return v2input.ErrInvalid
	}
	return nil
}

type nativeReadFenceStream struct {
	ctx    context.Context
	reader io.Reader
}

func (r nativeReadFenceStream) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if cancelled := r.ctx.Err(); cancelled != nil {
		return n, cancelled
	}
	return n, err
}
