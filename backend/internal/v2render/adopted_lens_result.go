package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type AdoptedLensObjectSource interface {
	LoadObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, string) (AdoptedLensBinding, objectstore.Object, error)
}

// LoadAdoptedLensResult performs object IO after SQL transactions close. The
// object reader must honor immutable VersionID and context cancellation. Child
// refs remain subject to separate scoped resolution; parsing does not grant IO.
func LoadAdoptedLensResult(ctx context.Context, source AdoptedLensObjectSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, version, producer uuid.UUID, expected v2input.Ref, bucket string) (AdoptedLensBinding, v2draft.ImageResultDocument, error) {
	empty := v2draft.ImageResultDocument{}
	if source == nil || objects == nil {
		return AdoptedLensBinding{}, empty, v2input.ErrInvalid
	}
	binding, obj, err := source.LoadObject(ctx, who, tenant, project, version, producer, expected, bucket)
	if err != nil {
		return AdoptedLensBinding{}, empty, err
	}
	if err := validateAdoptedLensBinding(binding, expected, version); err != nil {
		return AdoptedLensBinding{}, empty, err
	}
	if obj.Bytes < 2 || obj.Bytes > 1048576 || obj.SHA256 != expected.SHA256 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return AdoptedLensBinding{}, empty, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return AdoptedLensBinding{}, empty, err
	}
	body, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return AdoptedLensBinding{}, empty, readErr
	}
	if closeErr != nil {
		return AdoptedLensBinding{}, empty, closeErr
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != expected.SHA256 {
		return AdoptedLensBinding{}, empty, v2input.ErrInvalid
	}
	jcs, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(jcs, body) {
		return AdoptedLensBinding{}, empty, v2input.ErrInvalid
	}
	document, err := v2draft.ParseImageResult(body, expected)
	if err != nil {
		return AdoptedLensBinding{}, empty, err
	}
	if err := ctx.Err(); err != nil {
		return AdoptedLensBinding{}, empty, err
	}
	return binding, document, nil
}

func readAdoptedRoot(ctx context.Context, reader io.Reader, length int64) ([]byte, error) {
	var buffer bytes.Buffer
	limited := io.LimitReader(reader, length+1)
	block := make([]byte, 65536)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := limited.Read(block)
		if n > 0 {
			buffer.Write(block[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	if int64(buffer.Len()) != length {
		return nil, v2input.ErrInvalid
	}
	return buffer.Bytes(), nil
}
