package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type StyleCandidateLease struct {
	ID, Token, TenantID, RunID, StyleID uuid.UUID
	ConfigID, ReportID                  uuid.UUID
	Config, Report                      objectstore.Object
}
type StyleCandidateRepository interface {
	Claim(context.Context) (*StyleCandidateLease, error)
	Finish(context.Context, StyleCandidateLease, json.RawMessage, string, json.RawMessage, bool, string) (bool, error)
}
type SQLStyleCandidateRepository struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	Bucket string
}

func (r SQLStyleCandidateRepository) Claim(ctx context.Context) (*StyleCandidateLease, error) {
	if r.DB == nil || r.Bucket == "" {
		return nil, objectstore.ErrInvalid
	}
	var v StyleCandidateLease
	var configVersion, reportVersion *string
	var configBucket, reportBucket string
	err := r.DB.QueryRow(ctx, `SELECT candidate_id,token,tenant_id,run_id,style_id,config_id,config_bucket,config_sha,config_key,config_version,config_bytes,config_media,report_id,report_bucket,report_sha,report_key,report_version,report_bytes,report_media FROM photo.claim_style_candidate()`).Scan(&v.ID, &v.Token, &v.TenantID, &v.RunID, &v.StyleID, &v.ConfigID, &configBucket, &v.Config.SHA256, &v.Config.Key, &configVersion, &v.Config.Bytes, &v.Config.ContentType, &v.ReportID, &reportBucket, &v.Report.SHA256, &v.Report.Key, &reportVersion, &v.Report.Bytes, &v.Report.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if configVersion == nil || reportVersion == nil {
		return nil, objectstore.ErrIntegrity
	}
	v.Config.VersionID = *configVersion
	v.Report.VersionID = *reportVersion
	if v.ID == uuid.Nil || v.Token == uuid.Nil || v.TenantID == uuid.Nil || v.RunID == uuid.Nil || v.StyleID == uuid.Nil || v.ConfigID == uuid.Nil || v.ReportID == uuid.Nil || configBucket != r.Bucket || reportBucket != r.Bucket || v.Config.ContentType != "application/json" || v.Report.ContentType != "application/json" || v.Config.Bytes < 2 || v.Config.Bytes > 100000000 || v.Report.Bytes < 2 || v.Report.Bytes > 1048576 {
		return nil, objectstore.ErrIntegrity
	}
	return &v, nil
}
func (r SQLStyleCandidateRepository) Finish(ctx context.Context, v StyleCandidateLease, config json.RawMessage, hash string, metrics json.RawMessage, low bool, code string) (bool, error) {
	var ok bool
	var c any = config
	var m any = metrics
	if code != "" {
		c = nil
		m = nil
		hash = ""
	}
	err := r.DB.QueryRow(ctx, `SELECT photo.finish_style_candidate($1,$2,$3,$4,$5,$6,$7)`, v.ID, v.Token, c, hash, m, low, nullableString(code)).Scan(&ok)
	return ok, err
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

type StyleCandidateMaterializer struct {
	Repository StyleCandidateRepository
	Objects    ArtifactOpener
}

func readCandidateObject(ctx context.Context, objects ArtifactOpener, obj objectstore.Object, limit int64) ([]byte, error) {
	if objects == nil || obj.ContentType != "application/json" || obj.Bytes < 2 || obj.Bytes > limit {
		return nil, objectstore.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != obj.Bytes || int64(len(raw)) > limit || hex.EncodeToString(sum[:]) != obj.SHA256 {
		return nil, objectstore.ErrIntegrity
	}
	return canonical.Bytes(raw)
}
func (m StyleCandidateMaterializer) ProcessOne(ctx context.Context) (bool, error) {
	if m.Repository == nil || m.Objects == nil {
		return false, objectstore.ErrInvalid
	}
	lease, err := m.Repository.Claim(ctx)
	if err != nil || lease == nil {
		return false, err
	}
	fail := func(code string, cause error) (bool, error) {
		ok, e := m.Repository.Finish(ctx, *lease, nil, "", nil, true, code)
		if e != nil {
			return false, e
		}
		if !ok {
			return false, errors.New("style candidate lease lost")
		}
		return true, cause
	}
	config, err := readCandidateObject(ctx, m.Objects, lease.Config, 100000000)
	if err != nil {
		return fail("STYLE_CANDIDATE_INVALID", err)
	}
	report, err := readCandidateObject(ctx, m.Objects, lease.Report, 1048576)
	if err != nil {
		return fail("STYLE_REPORT_INVALID", err)
	}
	var style struct {
		SchemaVersion   string `json:"schema_version"`
		RendererVersion string `json:"renderer_version"`
		StyleID         string `json:"style_id"`
		Quality         struct {
			Status             string   `json:"status"`
			TransferConfidence string   `json:"transfer_confidence"`
			Warnings           []string `json:"warnings"`
		} `json:"quality"`
	}
	var evidence map[string]any
	if json.Unmarshal(config, &style) != nil || json.Unmarshal(report, &evidence) != nil || style.SchemaVersion != "2.0.0" || style.RendererVersion != "photo_renderer_v2" || style.StyleID != lease.StyleID.String() || evidence == nil {
		return fail("STYLE_CANDIDATE_INVALID", errors.New("invalid style candidate contract"))
	}
	hashBytes := sha256.Sum256(config)
	hash := hex.EncodeToString(hashBytes[:])
	if schema, _ := evidence["schema_version"].(string); schema == "style-import-report/v1" {
		configHash, configOK := evidence["config_hash"].(string)
		sourceHash, sourceOK := evidence["source_sha256"].(string)
		assets, assetsOK := evidence["assets"].([]any)
		_, sourceErr := hex.DecodeString(sourceHash)
		if !configOK || configHash != hash || !sourceOK || sourceErr != nil || len(sourceHash) != 64 || !assetsOK || len(assets) > 128 {
			return fail("STYLE_REPORT_INVALID", errors.New("invalid style import report binding"))
		}
	}
	low := style.Quality.Status != "measured" || style.Quality.TransferConfidence != "validated" || len(style.Quality.Warnings) > 0
	ok, err := m.Repository.Finish(ctx, *lease, config, hash, report, low, "")
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errors.New("style candidate lease lost")
	}
	return true, nil
}
