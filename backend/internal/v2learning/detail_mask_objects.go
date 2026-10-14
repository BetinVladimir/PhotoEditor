package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

const maxPortraitDetailMaskBytes int64 = 800065548

// SQLPortraitDetailMaskObjects verifies physical mask bytes without loading
// the potentially 800 MiB NPY into Go memory.
type SQLPortraitDetailMaskObjects struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func validPortraitDetailMaskObject(object objectstore.Object) bool {
	return object.Key != "" &&
		object.VersionID != "" && object.VersionID != "null" &&
		validSHA(object.SHA256) &&
		(object.ContentType == "application/octet-stream" ||
			object.ContentType == "application/x-npy") &&
		object.Bytes >= 32 && object.Bytes <= maxPortraitDetailMaskBytes
}

func (s SQLPortraitDetailMaskObjects) locate(ctx context.Context,
	start TrainingStart, eventID uuid.UUID, ref v2input.Ref,
) (objectstore.Object, error) {
	if s.DB == nil || s.Bucket == "" || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || eventID == uuid.Nil ||
		!validManifestRef(ref) {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v23_portrait_detail_mask_artifact(
 $1,$2,$3,$4,$5,$6,$7)`, start.TenantID, start.ProjectID, start.ActorID,
		start.AuthSessionID, eventID, ref.ArtifactID, ref.SHA256).Scan(
		&bucket, &object.Key, &object.VersionID, &object.SHA256,
		&object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != s.Bucket || !validPortraitDetailMaskObject(object) ||
		object.SHA256 != ref.SHA256 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	return object, nil
}

type detailMaskContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r detailMaskContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}

func hashPortraitDetailMask(ctx context.Context,
	reader LearningEvaluateObjectReader, object objectstore.Object,
	ref v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || !validManifestRef(ref) ||
		!validPortraitDetailMaskObject(object) ||
		object.SHA256 != ref.SHA256 {
		return ErrInvalidTrainingDraft
	}
	stream, err := reader.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return err
	}
	if stream == nil {
		return ErrInvalidTrainingDraft
	}
	digest := sha256.New()
	n, readErr := io.Copy(digest, io.LimitReader(
		detailMaskContextReader{ctx: ctx, reader: stream}, object.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if n != object.Bytes || hex.EncodeToString(digest.Sum(nil)) != ref.SHA256 {
		return ErrInvalidTrainingDraft
	}
	return nil
}

// Verify checks the SQL locator before and after one exact-version streaming
// read. It does not parse NPY shape; worker admission must do that separately.
func (s SQLPortraitDetailMaskObjects) Verify(ctx context.Context,
	start TrainingStart, eventID uuid.UUID, ref v2input.Ref) error {
	if s.Objects == nil {
		return ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, start, eventID, ref)
	if err != nil {
		return err
	}
	if err = hashPortraitDetailMask(ctx, s.Objects, before, ref); err != nil {
		return err
	}
	after, err := s.locate(ctx, start, eventID, ref)
	if err != nil {
		return err
	}
	if before != after {
		return ErrTrainingDraftConflict
	}
	return nil
}
