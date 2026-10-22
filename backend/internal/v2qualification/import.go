package v2qualification

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type ObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

type Importer struct {
	DB      *pgxpool.Pool // maintenance role only
	Objects ObjectReader
	Bucket  string
}

// Import reads the exact ready artifact version, verifies the deployment
// policy against its bytes, then lets the restricted SQL function recheck
// operator allowlisting and artifact state before immutable insertion.
func (s Importer) Import(ctx context.Context, tenantID, projectID,
	reportID uuid.UUID, reportSHA string, operatorID uuid.UUID,
	expected Expected) (uuid.UUID, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		tenantID == uuid.Nil || projectID == uuid.Nil || reportID == uuid.Nil ||
		operatorID == uuid.Nil || !digest(reportSHA) {
		return uuid.Nil, ErrUnqualified
	}
	var bucket string
	object := objectstore.Object{SHA256: reportSHA,
		ContentType: "application/json"}
	err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes
	 FROM photo.v2_qualification_report_object($1,$2,$3)`, tenantID,
		reportID, reportSHA).Scan(&bucket, &object.Key, &object.VersionID,
		&object.Bytes)
	if errors.Is(err, pgx.ErrNoRows) || bucket != s.Bucket ||
		object.Bytes < 2 || object.Bytes > 8*1024*1024 {
		return uuid.Nil, ErrUnqualified
	}
	if err != nil {
		return uuid.Nil, err
	}
	reader, err := s.Objects.Open(ctx, object)
	if err != nil {
		return uuid.Nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, 8*1024*1024+1))
	if err != nil || int64(len(body)) != object.Bytes {
		return uuid.Nil, ErrUnqualified
	}
	if err := VerifyImportReport(body, reportSHA, expected); err != nil {
		return uuid.Nil, err
	}
	var qualificationID uuid.UUID
	function := "photo.import_v2_qualification"
	if expected.MPSResources != nil {
		function = "photo.import_v2_mps_qualification"
	}
	err = s.DB.QueryRow(ctx, `SELECT `+function+`(
	 $1,$2,$3,$4,$5,$6,$7::text)`, tenantID, projectID, reportID,
		reportSHA, operatorID, expected.Backend, string(body)).Scan(&qualificationID)
	if err != nil {
		return uuid.Nil, err
	}
	if qualificationID == uuid.Nil {
		return uuid.Nil, ErrUnqualified
	}
	return qualificationID, nil
}
