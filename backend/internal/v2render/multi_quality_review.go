package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type MultiQualityReview struct {
	Proposal      MultiProposal   `json:"proposal"`
	QualityRun    uuid.UUID       `json:"quality_run_id"`
	QualityTask   uuid.UUID       `json:"quality_task_id"`
	QualityResult uuid.UUID       `json:"quality_result_id"`
	ReportRef     v2input.Ref     `json:"quality_report"`
	Report        json.RawMessage `json:"quality"`
}

func (s MultiProposalService) QualityReview(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, operation string) (MultiQualityReview, error) {
	var zero MultiQualityReview
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	proposal, err := s.Describe(ctx, who, tenantID, project, producer, operation)
	if err != nil {
		return zero, err
	}
	value := MultiQualityReview{Proposal: proposal}
	var raw string
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, "SELECT * FROM photo.v29_multi_quality_review($1,$2,$3,$4,$5,$6,$7,$8)", tenantID, project, operation, producer, who.UserID, who.SessionID, proposal.ResultID, proposal.Result.SHA256).Scan(&value.QualityRun, &value.QualityTask, &value.QualityResult, &value.ReportRef.ArtifactID, &value.ReportRef.SHA256, &raw)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	value.Report = json.RawMessage(raw)
	if err := validateMultiQualityReview(value); err != nil {
		return zero, err
	}
	return value, nil
}

func validateMultiQualityReview(v MultiQualityReview) error {
	if v.QualityRun == uuid.Nil || v.QualityTask != uuid.NewSHA1(v.QualityRun, []byte("quality.check:master")) || v.QualityResult == uuid.Nil || v.ReportRef.ArtifactID == uuid.Nil || len(v.Report) < 2 || len(v.Report) > 1048576 {
		return v2input.ErrInvalid
	}
	sum := sha256.Sum256(v.Report)
	if hex.EncodeToString(sum[:]) != v.ReportRef.SHA256 {
		return v2input.ErrInvalid
	}
	var report struct {
		Target v2input.Ref `json:"target"`
		Kind   string      `json:"target_kind"`
		Stage  string      `json:"stage"`
		Policy string      `json:"threshold_profile_id"`
		Status string      `json:"status"`
	}
	if json.Unmarshal(v.Report, &report) != nil || report.Target != v.Proposal.WorkingImage || report.Kind != "working" || report.Stage != "master" || report.Policy != "master_v1" {
		return v2input.ErrInvalid
	}
	switch report.Status {
	case "pass", "review", "fail":
		return nil
	default:
		return v2input.ErrInvalid
	}
}
