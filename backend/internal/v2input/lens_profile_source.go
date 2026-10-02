package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/objectstore"
)

type SQLLensProfileSource struct {
	DB                     AnalysisStageQuerier
	Objects                SessionAnalysisObjectReader
	Bucket, RuntimeLockSHA string
	QualificationPins      []LensQualificationPin
}

func (s SQLLensProfileSource) object(ctx context.Context, tenant, project, actor, session uuid.UUID, ref Ref, maxBytes int64) (objectstore.Object, error) {
	var obj objectstore.Object
	var bucket string
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,media_type FROM photo.v25_lens_profile_object($1,$2,$3,$4,$5,$6)`, tenant, project, actor, session, ref.ArtifactID, ref.SHA256).Scan(&bucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return obj, ErrExpired
	}
	if err != nil {
		return obj, err
	}
	if bucket != s.Bucket || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.Bytes < 2 || obj.Bytes > maxBytes || obj.ContentType != "application/json" {
		return objectstore.Object{}, ErrInvalid
	}
	obj.SHA256 = ref.SHA256
	return obj, nil
}

// Load returns only the exact profile bytes after its operator pin and the
// pinned report's actual versioned bytes are verified. Reports are streamed;
// their trusted qualification is established by operator pins, not by this IO.
func (s SQLLensProfileSource) Load(ctx context.Context, tenant, project, actor, session uuid.UUID, ref Ref) ([]byte, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || !validRef(ref) || !digestPattern.MatchString(s.RuntimeLockSHA) || len(s.QualificationPins) < 1 {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	obj, err := s.object(ctx, tenant, project, actor, session, ref, 1048576)
	if err != nil {
		return nil, err
	}
	reader, err := s.Objects.Open(ctx, obj)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != obj.Bytes {
		return nil, ErrInvalid
	}
	profile, err := VerifyQualifiedLensProfile(raw, ref, s.RuntimeLockSHA, s.QualificationPins)
	if err != nil {
		return nil, err
	}
	report, err := s.object(ctx, tenant, project, actor, session, profile.Qualification, 8388608)
	if err != nil {
		return nil, err
	}
	if err = s.verifyReport(ctx, report); err != nil {
		return nil, err
	}
	return raw, nil
}
func (s SQLLensProfileSource) verifyReport(ctx context.Context, obj objectstore.Object) (err error) {
	reader, err := s.Objects.Open(ctx, obj)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := reader.Close()
		if err == nil {
			err = closeErr
		}
	}()
	hash := sha256.New()
	buffer := make([]byte, 65536)
	var total int64
	emptyReads := 0
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			emptyReads = 0
			total += int64(n)
			if total > obj.Bytes {
				return ErrInvalid
			}
			hash.Write(buffer[:n])
		} else if readErr == nil {
			emptyReads++
			if emptyReads >= 100 {
				return io.ErrNoProgress
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if total != obj.Bytes || hex.EncodeToString(hash.Sum(nil)) != obj.SHA256 {
		return ErrInvalid
	}
	return ctx.Err()
}
