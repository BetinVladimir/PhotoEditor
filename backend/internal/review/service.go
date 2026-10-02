package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"strings"
	"time"
)

type Objects interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

type Service struct {
	DB      *pgxpool.Pool
	Objects Objects
	Bucket  string
	Gate    DefectGate
	Cursors pagination.Codec
}

type View struct {
	ID            uuid.UUID       `json:"id"`
	RunID         uuid.UUID       `json:"run_id"`
	Kind          string          `json:"kind"`
	State         string          `json:"state"`
	Revision      int64           `json:"revision"`
	CandidateHash string          `json:"candidate_hash"`
	Decision      *string         `json:"decision,omitempty"`
	DecisionRoute *string         `json:"decision_route,omitempty"`
	Reasons       json.RawMessage `json:"reasons"`
	CreatedAt     time.Time       `json:"created_at"`
}
type DetailView struct {
	View
	CandidateIDs []string `json:"candidate_ids,omitempty"`
}

func (s *Service) Detail(ctx context.Context, who auth.Identity, tid, reviewID uuid.UUID) (DetailView, error) {
	var out DetailView
	if s == nil || s.DB == nil || s.Objects == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var loaded decisionContext
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var e error
		loaded, e = loadDecisionContext(ctx, tx, tid, reviewID, s.Bucket)
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tid, loaded.ProjectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT id,run_id,kind,state,revision,candidate_hash,decision,decision_route,reasons,created_at FROM photo.reviews WHERE tenant_id=$1 AND id=$2`, tid, reviewID).Scan(&out.ID, &out.RunID, &out.Kind, &out.State, &out.Revision, &out.CandidateHash, &out.Decision, &out.DecisionRoute, &out.Reasons, &out.CreatedAt)
	})
	if err != nil {
		return out, err
	}
	raw, err := readObject(ctx, s.Objects, loaded.ProposalObject)
	if err != nil {
		return out, err
	}
	if loaded.Kind == "sensor_dust" {
		proposal, e := ReadDustProposal(ctx, bytes.NewReader(raw), loaded.Proposal, loaded.Image)
		if e != nil {
			return out, domain.NewError("INPUT_INVALID", 422)
		}
		out.CandidateIDs = make([]string, len(proposal.document.Candidates))
		for i, c := range proposal.document.Candidates {
			out.CandidateIDs[i] = c.ID
		}
	}
	if loaded.Kind == "defects" {
		baseRaw, e := readObject(ctx, s.Objects, loaded.ImageObject)
		if e != nil {
			return out, e
		}
		image, e := ParseWorkingImage(baseRaw)
		if e != nil {
			return out, domain.NewError("INPUT_INVALID", 422)
		}
		proposal, e := ReadDefectProposal(ctx, bytes.NewReader(raw), loaded.Proposal, image.Source(loaded.Image))
		if e != nil {
			return out, domain.NewError("INPUT_INVALID", 422)
		}
		out.CandidateIDs = make([]string, len(proposal.document.Candidates))
		for i, c := range proposal.document.Candidates {
			out.CandidateIDs[i] = c.ID
		}
	}
	return out, nil
}

type decisionContext struct {
	ProjectID      uuid.UUID
	Kind           string
	Proposal       ArtifactRef
	Image          ArtifactRef
	ProposalObject objectstore.Object
	ImageObject    objectstore.Object
}

func artifactObject(id uuid.UUID, bucket, key, version, sha, media string, size int64) (ArtifactRef, objectstore.Object, error) {
	if id == uuid.Nil || bucket == "" || key == "" || version == "" || version == "null" || media != "application/json" || size < 2 || size > 1048576 {
		return ArtifactRef{}, objectstore.Object{}, domain.NewError("ARTIFACT_MISSING", 409)
	}
	ref := ArtifactRef{ID: id, SHA256: sha}
	if !ref.valid() {
		return ArtifactRef{}, objectstore.Object{}, domain.NewError("ARTIFACT_HASH_MISMATCH", 409)
	}
	return ref, objectstore.Object{Key: key, VersionID: version, SHA256: sha, ContentType: media, Bytes: size}, nil
}

func loadDecisionContext(ctx context.Context, tx pgx.Tx, tid, reviewID uuid.UUID, expectedBucket string) (decisionContext, error) {
	var c decisionContext
	var proposalID, imageID uuid.UUID
	var pb, pk, pv, ps, pm, ib, ik, iv, is, im string
	var pn, in int64
	err := tx.QueryRow(ctx, `SELECT coalesce(src.project_id,dst.project_id),v.kind,v.candidate_artifact_id,v.base_artifact_id,
 p.bucket,p.object_key,p.object_version,p.sha256,p.media_type,p.bytes,
 i.bucket,i.object_key,i.object_version,i.sha256,i.media_type,i.bytes
 FROM photo.reviews v JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 LEFT JOIN photo.collections src ON src.tenant_id=s.tenant_id AND src.id=s.collection_id
 LEFT JOIN photo.collections dst ON dst.tenant_id=r.tenant_id AND dst.id=r.output_collection_id
 JOIN photo.artifacts p ON p.tenant_id=v.tenant_id AND p.id=v.candidate_artifact_id
 JOIN photo.artifacts i ON i.tenant_id=v.tenant_id AND i.id=v.base_artifact_id
 WHERE v.tenant_id=$1 AND v.id=$2 AND v.state='pending'
 AND p.state='ready' AND i.state='ready'
 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
	AND (i.expires_at IS NULL OR i.expires_at>clock_timestamp())`, tid, reviewID).Scan(&c.ProjectID, &c.Kind, &proposalID, &imageID,
		&pb, &pk, &pv, &ps, &pm, &pn, &ib, &ik, &iv, &is, &im, &in)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return c, err
	}
	if expectedBucket == "" || pb != expectedBucket || ib != expectedBucket {
		return c, domain.NewError("ARTIFACT_MISSING", 409)
	}
	c.Proposal, c.ProposalObject, err = artifactObject(proposalID, pb, pk, pv, ps, pm, pn)
	if err != nil {
		return c, err
	}
	c.Image, c.ImageObject, err = artifactObject(imageID, ib, ik, iv, is, im, in)
	return c, err
}

func readObject(ctx context.Context, objects Objects, object objectstore.Object) ([]byte, error) {
	reader, err := objects.Open(ctx, object)
	if err != nil {
		return nil, domain.NewError("ARTIFACT_MISSING", 409)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 1048577))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1048576 {
		return nil, domain.NewError("INPUT_INVALID", 422)
	}
	digest := sha256.Sum256(raw)
	if int64(len(raw)) != object.Bytes || hex.EncodeToString(digest[:]) != object.SHA256 {
		return nil, domain.NewError("ARTIFACT_HASH_MISMATCH", 409)
	}
	return raw, nil
}

func sameContext(a, b decisionContext) bool {
	return a.ProjectID == b.ProjectID && a.Kind == b.Kind && a.Proposal == b.Proposal && a.Image == b.Image &&
		a.ProposalObject == b.ProposalObject && a.ImageObject == b.ImageObject
}

func compatibleCandidate(kind string, candidate, base WorkingImage) bool {
	if candidate.Color != base.Color || candidate.DType != base.DType || candidate.Canvas.SourceSHA256 != base.Canvas.SourceSHA256 {
		return false
	}
	if kind != "composition" {
		return candidate.Canvas == base.Canvas && bytes.Equal(candidate.TransformChain, base.TransformChain)
	}
	var c, b transformChain
	if json.Unmarshal(candidate.TransformChain, &c) != nil || json.Unmarshal(base.TransformChain, &b) != nil || len(c.Steps) != len(b.Steps)+1 {
		return false
	}
	for i := range b.Steps {
		left, _ := canonical.Marshal(c.Steps[i])
		right, _ := canonical.Marshal(b.Steps[i])
		if !bytes.Equal(left, right) {
			return false
		}
	}
	last := c.Steps[len(c.Steps)-1]
	return last.Kind == "matrix" && last.From == base.Canvas && last.To == candidate.Canvas
}

func (s *Service) Decide(ctx context.Context, who auth.Identity, tid, reviewID, requestID uuid.UUID, req Decision) (View, error) {
	var out View
	if s == nil || s.DB == nil || s.Objects == nil || s.Gate == nil || requestID == uuid.Nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var loaded decisionContext
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var err error
		loaded, err = loadDecisionContext(ctx, tx, tid, reviewID, s.Bucket)
		if err != nil {
			return err
		}
		_, err = project.Authorize(ctx, tx, tid, loaded.ProjectID, who.UserID, role, domain.Editor, false)
		return err
	})
	if err != nil {
		return out, err
	}
	imageRaw, err := readObject(ctx, s.Objects, loaded.ImageObject)
	if err != nil {
		return out, err
	}
	image, err := ParseWorkingImage(imageRaw)
	if err != nil {
		return out, domain.NewError("INPUT_INVALID", 422)
	}
	proposalRaw, err := readObject(ctx, s.Objects, loaded.ProposalObject)
	if err != nil {
		return out, err
	}
	var proposal VerifiedProposal
	var dust VerifiedDustProposal
	if loaded.Kind == "defects" {
		proposal, err = ReadDefectProposal(ctx, bytes.NewReader(proposalRaw), loaded.Proposal, image.Source(loaded.Image))
	} else if loaded.Kind == "sensor_dust" {
		dust, err = ReadDustProposal(ctx, bytes.NewReader(proposalRaw), loaded.Proposal, loaded.Image)
	} else {
		var candidate WorkingImage
		candidate, err = ParseWorkingImage(proposalRaw)
		if err == nil && !compatibleCandidate(loaded.Kind, candidate, image) {
			err = errors.New("candidate/base geometry mismatch")
		}
	}
	if err != nil {
		return out, domain.NewError("INPUT_INVALID", 422)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		current, err := loadDecisionContext(ctx, tx, tid, reviewID, s.Bucket)
		if err != nil {
			return err
		}
		if !sameContext(loaded, current) {
			return domain.NewError("STALE_DEFECT_REVIEW", 409)
		}
		if _, err = project.Authorize(ctx, tx, tid, current.ProjectID, who.UserID, role, domain.Editor, true); err != nil {
			return err
		}
		if loaded.Kind == "defects" {
			plan, persistErr := PersistDefectsAuthorized(ctx, tx, tid, reviewID, who.UserID, requestID, proposal, req, s.Gate)
			if persistErr != nil {
				return persistErr
			}
			out = View{ID: reviewID, Kind: loaded.Kind, State: "decided", Revision: plan.Revision, CandidateHash: loaded.Proposal.SHA256, Decision: &req.Decision, DecisionRoute: &plan.Route}
		} else if loaded.Kind == "sensor_dust" {
			plan, persistErr := PersistDustAuthorized(ctx, tx, tid, reviewID, who.UserID, requestID, dust, req)
			if persistErr != nil {
				return persistErr
			}
			out = View{ID: reviewID, Kind: loaded.Kind, State: "decided", Revision: plan.Revision, CandidateHash: loaded.Proposal.SHA256, Decision: &req.Decision, DecisionRoute: &plan.Route}
		} else {
			plan, persistErr := PersistCandidateAuthorized(ctx, tx, tid, reviewID, who.UserID, requestID, req)
			if persistErr != nil {
				return persistErr
			}
			out = View{ID: reviewID, Kind: loaded.Kind, State: "decided", Revision: plan.Revision, CandidateHash: loaded.Proposal.SHA256, Decision: &req.Decision, DecisionRoute: &plan.Route}
		}
		return tx.QueryRow(ctx, `SELECT run_id FROM photo.reviews WHERE tenant_id=$1 AND id=$2`, tid, reviewID).Scan(&out.RunID)
	})
	return out, err
}

func (s *Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw, runRaw, state, kind string) (pagination.Page[View], error) {
	out := pagination.Page[View]{Items: []View{}}
	if s == nil || s.DB == nil || limit < 1 || limit > 100 ||
		(state != "" && state != "pending" && state != "decided") || len(kind) > 64 || strings.ContainsAny(kind, "\x00\r\n") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var runID *uuid.UUID
	if runRaw != "" {
		parsed, err := uuid.Parse(runRaw)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		runID = &parsed
	}
	filterHash, err := canonical.Hash(map[string]string{"run_id": runRaw, "state": state, "kind": kind})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: filterHash, Sort: "review:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, err := uuid.Parse(cursor.LastID)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		position, err := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID, afterTime = &id, &position
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT v.id,v.run_id,v.kind,v.state,v.revision,v.candidate_hash,v.decision,v.decision_route,v.reasons,v.created_at
 FROM photo.reviews v JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 LEFT JOIN photo.collections src ON src.tenant_id=s.tenant_id AND src.id=s.collection_id
 LEFT JOIN photo.collections dst ON dst.tenant_id=r.tenant_id AND dst.id=r.output_collection_id
 JOIN photo.projects p ON p.tenant_id=v.tenant_id AND p.id=coalesce(src.project_id,dst.project_id) AND p.archived_at IS NULL
 WHERE v.tenant_id=$1 AND ($2::uuid IS NULL OR v.run_id=$2) AND ($3='' OR v.state=$3) AND ($4='' OR v.kind=$4)
 AND ($5 OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl a WHERE a.tenant_id=p.tenant_id AND a.project_id=p.id AND a.user_id=$6 AND a.role='editor'))
 AND ($7::uuid IS NULL OR (v.created_at,v.id)>($8::timestamptz,$7::uuid))
 ORDER BY v.created_at,v.id LIMIT $9`, tid, runID, state, kind, role.Allows(domain.Admin), who.UserID, afterID, afterTime, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item View
			if err = rows.Scan(&item.ID, &item.RunID, &item.Kind, &item.State, &item.Revision, &item.CandidateHash, &item.Decision, &item.DecisionRoute, &item.Reasons, &item.CreatedAt); err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID, scope.LastPosition = last.ID.String(), last.CreatedAt.Format(time.RFC3339Nano)
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}
