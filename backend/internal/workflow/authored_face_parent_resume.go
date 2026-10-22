package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// Concrete preparation keeps persisted selection separate from inference. A
// pending/rejected decision never produces a skin task or a completed phase.
type AuthoredFaceParentPreparation struct {
	Scheduler    *pgxpool.Pool
	Models       v2render.AuthoredFacePreparation
	Candidate    v2render.AuthoredFaceCandidatePublication
	Preview      AuthoredFacePreviewRuntime
	Skin         v2render.AuthoredSkinPreparation
	PhaseObjects v2render.SQLAuthoredPhaseObjects
	Deployment   AuthoredFaceDeployment
}
type AuthoredFaceParentProgress struct {
	State        string
	CandidateSHA string
	Advanced     bool
}

func NewAuthoredFaceParentPreparation(scheduler, api *pgxpool.Pool, objects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}, bucket, srgb string, deployment AuthoredFaceDeployment) (*AuthoredFaceParentPreparation, error) {
	if scheduler == nil || api == nil || objects == nil || bucket == "" || !deploymentDigest(srgb) {
		return nil, ErrAuthoredFaceUnavailable
	}
	if err := deployment.validate(); err != nil {
		return nil, err
	}
	raw, err := canonical.Marshal(deployment)
	if err != nil {
		return nil, ErrAuthoredFaceUnavailable
	}
	var owned AuthoredFaceDeployment
	if json.Unmarshal(raw, &owned) != nil {
		return nil, ErrAuthoredFaceUnavailable
	}
	deployment = owned
	producers := v2render.AuthoredFaceProducerReader{Scheduler: scheduler, Objects: objects, Bucket: bucket}
	phase := v2render.SQLAuthoredPhaseObjects{API: api, Objects: objects, Bucket: bucket}
	return &AuthoredFaceParentPreparation{Scheduler: scheduler, Deployment: deployment, PhaseObjects: phase,
		Models:    v2render.AuthoredFacePreparation{Scheduler: scheduler, Qualification: v2render.SQLAuthoredFaceQualification{API: api, Objects: objects, Bucket: bucket}, Params: v2render.AuthoredFaceParamsPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Producers: producers},
		Candidate: v2render.AuthoredFaceCandidatePublication{Producers: producers, PhaseObjects: phase},
		Preview:   AuthoredFacePreviewRuntime{DB: scheduler, Objects: objects, Bucket: bucket, SRGBSHA: srgb, Policy: deployment.PreviewPolicy},
		Skin:      v2render.AuthoredSkinPreparation{Scheduler: scheduler, Qualification: v2render.SQLAuthoredSkinQualification{API: api, Objects: objects, Bucket: bucket}, Params: v2render.AuthoredSkinParamsPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Producers: producers}}, nil
}
func (p *AuthoredFaceParentPreparation) Resume(ctx context.Context, tenant, parent uuid.UUID, ordinal int) (AuthoredFaceParentProgress, error) {
	var out AuthoredFaceParentProgress
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if p == nil || p.Scheduler == nil {
		return out, ErrAuthoredFaceUnavailable
	}
	source, ok, err := v2render.ReadAuthoredInstructionSource(ctx, p.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return out, err
	}
	if !ok {
		return out, v2input.ErrExpired
	}
	who := auth.Identity{UserID: source.Actor, SessionID: source.Session}
	image, err := p.PhaseObjects.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, source.Source.Source)
	if err != nil {
		return out, err
	}
	for _, stage := range []AuthoredFaceOperationDeployment{p.Deployment.Detector, p.Deployment.Landmarks, p.Deployment.Parser} {
		advanced, e := p.Models.ResumePrerequisite(ctx, who, source, image, stage.Expected, stage.Report, p.Deployment.Score, p.Deployment.MaxFaces, stage.budget())
		if errors.Is(e, v2render.ErrAuthoredFacePending) {
			return AuthoredFaceParentProgress{State: "models_pending", Advanced: advanced}, nil
		}
		if e != nil {
			return out, e
		}
	}
	candidate, sha, err := p.Candidate.Publish(ctx, who, source)
	if err != nil {
		return out, err
	}
	// Preview remains a real separately adopted derivative; its queued receipt is
	// never returned as a verified media capability or as approval of this source.
	previewState, err := p.Preview.Resume(ctx, sha, candidate, image)
	if err != nil {
		return out, err
	}
	if previewState != "adopted" {
		return AuthoredFaceParentProgress{State: "preview_pending", CandidateSHA: sha}, nil
	}
	var decision *string
	err = database.TenantTransaction(ctx, p.Scheduler, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.authored_render_face_decision_state($1,$2,$3,$4)", tenant, parent, ordinal, sha).Scan(&decision)
	})
	if err != nil {
		return out, err
	}
	if decision == nil {
		return out, v2input.ErrExpired
	}
	switch *decision {
	case "needs_review":
		return AuthoredFaceParentProgress{State: "needs_review", CandidateSHA: sha}, nil
	case "rejected":
		return AuthoredFaceParentProgress{State: "rejected", CandidateSHA: sha}, nil
	case "accepted":
		s := p.Deployment.Skin
		_, err = p.Skin.Resume(ctx, who, source, image, sha, s.Expected, s.Report, s.budget())
		if errors.Is(err, v2render.ErrAuthoredSkinPending) {
			return AuthoredFaceParentProgress{State: "skin_pending", CandidateSHA: sha}, nil
		}
		if err != nil {
			return out, err
		}
		// Actual canonical skin readback is now available to the layer materializer.
		// This preparation result neither commits a layer nor settles parent review.
		return AuthoredFaceParentProgress{State: "skin_adopted", CandidateSHA: sha}, nil
	default:
		return out, v2input.ErrInvalid
	}
}
