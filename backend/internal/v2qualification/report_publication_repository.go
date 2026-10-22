// Maintenance-only adapter for the qualification-report publication protocol.
// RequireReportPublicationSchema must attest its installed SQL authority before use.
package v2qualification

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

type SQLReportPublicationRepository struct {
	DB     *pgxpool.Pool
	Bucket string
}

func reportScopeArgs(s ReportPublicationScope, t ReportPublicationReservation) []any {
	return []any{s.Tenant, s.Project, s.Operator, s.Key, t.Artifact, t.Token}
}

// Publisher keeps raw bytes through this private wrapper so SQL repeats exact
// policy/evidence authorization against those same bytes (not a caller boolean).
type reportBodyRepository struct {
	SQLReportPublicationRepository
	Body string
}

func (r reportBodyRepository) Reserve(ctx context.Context, s ReportPublicationScope, e Expected, sha string, size int64) (ReportPublicationReservation, error) {
	var t ReportPublicationReservation
	var version *string
	if r.DB == nil {
		return t, ErrUnqualified
	}
	err := r.DB.QueryRow(ctx, `SELECT artifact,token,writer_state,object_version FROM photo.reserve_qualification_report($1,$2,$3,$4,$5,$6,$7)`, s.Tenant, s.Project, s.Operator, s.Key, e.Backend, r.Body, r.Bucket).Scan(&t.Artifact, &t.Token, &t.WriterState, &version)
	if err != nil {
		return t, err
	}
	// Closed pending writer still requires Complete; distinguish ready by Check
	// query rather than treating any known object version as registered artifact.
	var ready bool
	err = r.DB.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.v2_qualification_report_object($1,$2,$3))`, s.Tenant, t.Artifact, sha).Scan(&ready)
	if err != nil {
		return t, err
	}
	if version != nil {
		t.Pinned = &objectstore.Object{Key: reportPublicationKey(s, t.Artifact), VersionID: *version, SHA256: sha, Bytes: size, ContentType: "application/json"}
	}
	if ready {
		if t.Pinned == nil {
			return t, objectstore.ErrIntegrity
		}
		copy := *t.Pinned
		t.Ready = &copy
	}
	return t, nil
}
func (r SQLReportPublicationRepository) Check(ctx context.Context, s ReportPublicationScope, e Expected, t ReportPublicationReservation, sha string, size int64) error {
	args := reportScopeArgs(s, t)
	var ok bool
	if err := r.DB.QueryRow(ctx, `SELECT photo.check_qualification_report($1,$2,$3,$4,$5,$6)`, args...).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrUnqualified
	}
	return nil
}
func (r SQLReportPublicationRepository) Start(ctx context.Context, s ReportPublicationScope, t ReportPublicationReservation) error {
	var ok bool
	if err := r.DB.QueryRow(ctx, `SELECT photo.start_qualification_report_write($1,$2,$3,$4,$5,$6)`, reportScopeArgs(s, t)...).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrUnqualified
	}
	return nil
}
func (r SQLReportPublicationRepository) Close(ctx context.Context, s ReportPublicationScope, t ReportPublicationReservation, o objectstore.Object) error {
	var ok bool
	args := append(reportScopeArgs(s, t), o.VersionID)
	if err := r.DB.QueryRow(ctx, `SELECT photo.close_qualification_report_write($1,$2,$3,$4,$5,$6,$7)`, args...).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrUnqualified
	}
	return nil
}
func (r SQLReportPublicationRepository) Complete(ctx context.Context, s ReportPublicationScope, e Expected, t ReportPublicationReservation, o objectstore.Object) (artifactjson.Ref, error) {
	var ok bool
	args := append(reportScopeArgs(s, t), o.SHA256, o.Bytes, r.Bucket, o.Key, o.VersionID)
	if err := r.DB.QueryRow(ctx, `SELECT photo.complete_qualification_report($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, args...).Scan(&ok); err != nil {
		return artifactjson.Ref{}, err
	}
	if !ok {
		return artifactjson.Ref{}, ErrUnqualified
	}
	return artifactjson.Ref{ID: t.Artifact, SHA256: o.SHA256}, nil
}

// Compile-time interface assertion, not execution evidence.
var _ ReportPublicationRepository = reportBodyRepository{}
