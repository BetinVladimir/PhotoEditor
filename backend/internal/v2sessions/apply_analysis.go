package v2sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/run"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type analysisApplyItem struct {
	Ordinal        int
	ItemID         uuid.UUID
	AssetVersionID uuid.UUID
	ArtifactID     uuid.UUID
	SHA256         string
}

type analysisApplyContext struct {
	ProjectID        uuid.UUID
	ParentRunID      uuid.UUID
	WorkflowID       uuid.UUID
	SnapshotID       uuid.UUID
	SnapshotDigest   string
	PolicySHA        string
	ItemCount        int
	SourceCollection uuid.UUID
	SourceRevision   int64
	Plan             sceneObject
	Input            sceneObject
	StyleVersionID   uuid.UUID
	StyleConfig      json.RawMessage
	Items            []analysisApplyItem
	Corrections      map[uuid.UUID]sceneObject
}

func scanAnalysisObject(row pgx.Row) (sceneObject, error) {
	var source sceneObject
	err := row.Scan(&source.ID, &source.Object.SHA256, &source.Object.Bytes,
		&source.Bucket, &source.Object.Key, &source.Object.VersionID,
		&source.Object.ContentType)
	return source, err
}

func (s Service) analysisApplyContext(ctx context.Context, who auth.Identity,
	tenantID, analysisID uuid.UUID, recipe ArtifactRef) (analysisApplyContext, error) {
	var out analysisApplyContext
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			var status string
			err := tx.QueryRow(ctx, `SELECT a.project_id,w.run_id,w.id,a.snapshot_id,
			 s.digest,s.item_count,w.policy_sha,shoot.collection_id,s.source_revision,
			 parent.status,plan.id,plan.sha256,plan.bytes,plan.bucket,
			 plan.object_key,plan.object_version,plan.media_type
			 FROM photo.session_analyses a
			 JOIN photo.v2_workflows w ON w.tenant_id=a.tenant_id AND w.id=a.workflow_id
			 JOIN photo.runs parent ON parent.tenant_id=w.tenant_id AND parent.id=w.run_id
			 JOIN photo.snapshots s ON s.tenant_id=a.tenant_id AND s.id=a.snapshot_id
			 JOIN photo.shoot_sessions shoot ON shoot.tenant_id=a.tenant_id
			  AND shoot.id=a.session_id AND shoot.project_id=a.project_id
			 JOIN photo.collections source ON source.tenant_id=shoot.tenant_id
			  AND source.id=shoot.collection_id AND source.project_id=a.project_id
			 JOIN photo.artifacts plan ON plan.tenant_id=a.tenant_id
			  AND plan.id=a.plan_artifact_id
			 WHERE a.tenant_id=$1 AND a.id=$2 AND w.workflow_type='session'
			  AND w.renderer_version='v2.2' AND w.input_snapshot_id=a.snapshot_id
			  AND parent.kind='v2' AND NOT parent.cancel_requested
			  AND s.kind='v2' AND s.collection_id=shoot.collection_id
			  AND s.source_revision IS NOT NULL
			  AND plan.state='ready' AND plan.output_role='document'
			  AND plan.media_type='application/json' AND plan.verified_at IS NOT NULL
			  AND plan.object_version IS NOT NULL AND plan.object_version NOT IN('','null')
			  AND (plan.expires_at IS NULL OR plan.expires_at>clock_timestamp())`,
				tenantID, analysisID).Scan(&out.ProjectID, &out.ParentRunID,
				&out.WorkflowID, &out.SnapshotID, &out.SnapshotDigest,
				&out.ItemCount, &out.PolicySHA, &out.SourceCollection,
				&out.SourceRevision, &status, &out.Plan.ID,
				&out.Plan.Object.SHA256, &out.Plan.Object.Bytes,
				&out.Plan.Bucket, &out.Plan.Object.Key,
				&out.Plan.Object.VersionID, &out.Plan.Object.ContentType)
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			if err != nil {
				return err
			}
			if _, err = project.Authorize(ctx, tx, tenantID, out.ProjectID,
				who.UserID, role, domain.Editor, false); err != nil {
				return err
			}
			if status != "waiting_review" && status != "completed" ||
				out.ItemCount < 1 || out.ItemCount > 5000 {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			out.Input, err = scanAnalysisObject(tx.QueryRow(ctx,
				`SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
				 FROM photo.v22_analysis_apply_input_source($1,$2,$3,$4)`,
				tenantID, analysisID, who.UserID, who.SessionID))
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			if err != nil {
				return err
			}
			styles, err := tx.Query(ctx, `SELECT v.id,v.config_json,v.config_hash
			 FROM photo.style_versions v JOIN photo.styles st
			 ON st.tenant_id=v.tenant_id AND st.id=v.style_id
			 WHERE v.tenant_id=$1 AND v.export_artifact_id=$2
			 AND v.config_hash=$3 AND st.project_id=$4`, tenantID,
				recipe.ArtifactID, recipe.SHA256, out.ProjectID)
			if err != nil {
				return err
			}
			styleCount := 0
			var styleHash string
			for styles.Next() {
				if err = styles.Scan(&out.StyleVersionID, &out.StyleConfig,
					&styleHash); err != nil {
					break
				}
				styleCount++
			}
			if err == nil {
				err = styles.Err()
			}
			styles.Close()
			if err != nil {
				return err
			}
			if styleCount != 1 {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			actualStyleSHA, err := canonical.Hash(json.RawMessage(out.StyleConfig))
			if err != nil || actualStyleSHA != styleHash || styleHash != recipe.SHA256 {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			items, err := tx.Query(ctx, `SELECT ordinal,item_id,asset_version_id,
			 artifact_id,sha256 FROM photo.snapshot_items
			 WHERE tenant_id=$1 AND snapshot_id=$2 ORDER BY ordinal`,
				tenantID, out.SnapshotID)
			if err != nil {
				return err
			}
			for items.Next() {
				var item analysisApplyItem
				if err = items.Scan(&item.Ordinal, &item.ItemID,
					&item.AssetVersionID, &item.ArtifactID, &item.SHA256); err != nil {
					break
				}
				out.Items = append(out.Items, item)
			}
			if err == nil {
				err = items.Err()
			}
			items.Close()
			if err != nil {
				return err
			}
			if len(out.Items) != out.ItemCount {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			out.Corrections = make(map[uuid.UUID]sceneObject)
			rows, err := tx.Query(ctx, `SELECT artifact.id,artifact.sha256,
			 artifact.bytes,artifact.bucket,artifact.object_key,
			 artifact.object_version,artifact.media_type
			 FROM photo.v2_artifact_links link JOIN photo.artifacts artifact
			 ON artifact.tenant_id=link.tenant_id AND artifact.id=link.artifact_id
			 AND artifact.sha256=link.sha256
			 WHERE link.tenant_id=$1 AND link.workflow_id=$2
			 AND artifact.state='ready' AND artifact.output_role='correction_plan'
			 AND artifact.media_type='application/json' AND artifact.verified_at IS NOT NULL
			 AND artifact.object_version IS NOT NULL
			 AND artifact.object_version NOT IN('','null')
			 AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())`,
				tenantID, out.WorkflowID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var source sceneObject
				if err = rows.Scan(&source.ID, &source.Object.SHA256,
					&source.Object.Bytes, &source.Bucket,
					&source.Object.Key, &source.Object.VersionID,
					&source.Object.ContentType); err != nil {
					break
				}
				if _, duplicate := out.Corrections[source.ID]; duplicate {
					err = domain.NewError("ANALYSIS_CONFLICT", 409)
					break
				}
				out.Corrections[source.ID] = source
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			return err
		})
	return out, err
}

func validatedAnalysisBindings(raw []byte, context analysisApplyContext) (
	map[int]v2input.SessionVersionBinding, error) {
	if !exactCanonicalJSON(raw, context.Input.Object.SHA256, 8<<20) {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	var snapshot v2input.SessionInputSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(new(any)) != io.EOF ||
		snapshot.SnapshotID != context.SnapshotID ||
		snapshot.SnapshotDigest != context.SnapshotDigest ||
		len(snapshot.Items) != context.ItemCount {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	compiled, body, err := v2input.CompileSessionInputSnapshot(context.ParentRunID,
		snapshot.Items)
	if err != nil || compiled.SnapshotDigest != context.SnapshotDigest ||
		!bytes.Equal(body, raw) {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	bindings := make(map[int]v2input.SessionVersionBinding, len(snapshot.Items))
	for ordinal, item := range snapshot.Items {
		relational := context.Items[ordinal]
		if item.Ordinal != ordinal || relational.Ordinal != ordinal ||
			relational.ItemID == uuid.Nil ||
			item.Binding.AssetVersionID != relational.AssetVersionID ||
			item.Binding.Source.ArtifactID != relational.ArtifactID ||
			item.Binding.Source.SHA256 != relational.SHA256 {
			return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
		}
		bindings[ordinal] = item.Binding
	}
	return bindings, nil
}

func correctionVectors(raw []byte, group sceneGroup, snapshotDigest string,
	bindings map[int]v2input.SessionVersionBinding) (map[int]v2draft.CorrectionVector, error) {
	if err := validateCorrectionPlan(raw, group.Corrections,
		snapshotDigest, group, bindings); err != nil {
		return nil, err
	}
	var doc struct {
		Items []struct {
			Ordinal     int                      `json:"ordinal"`
			Delta       v2draft.CorrectionVector `json:"delta"`
			Disposition string                   `json:"disposition"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	out := make(map[int]v2draft.CorrectionVector, len(doc.Items))
	for _, item := range doc.Items {
		if item.Disposition == "apply" {
			out[item.Ordinal] = item.Delta
		} else {
			out[item.Ordinal] = v2draft.CorrectionVector{}
		}
	}
	return out, nil
}

// A sealed retry must survive expiry of the source objects and later style
// changes. The immutable decision and the original idempotency response are
// sufficient after rechecking the current editor ACL.
func (s Service) analysisApplyReplay(ctx context.Context, who auth.Identity,
	tenantID, analysisID uuid.UUID, key, decisionSHA string) (v2input.JobTicket, bool, error) {
	var ticket v2input.JobTicket
	var found bool
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, _ domain.Role) error {
			var outcome string
			var child *uuid.UUID
			var workflowID uuid.UUID
			err := tx.QueryRow(ctx, `SELECT outcome,child_run_id,workflow_id
			 FROM photo.v22_analysis_apply_replay($1,$2,$3,$4,$5,$6)`,
				tenantID, analysisID, who.UserID, who.SessionID, key,
				decisionSHA).Scan(&outcome, &child, &workflowID)
			if err != nil {
				return analysisError(err)
			}
			switch outcome {
			case "absent":
				return nil
			case "conflict":
				return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
			case "replayed":
				if child == nil || *child == uuid.Nil || workflowID == uuid.Nil {
					return domain.NewError("ANALYSIS_CONFLICT", 409)
				}
				ticket = v2input.JobTicket{RunID: *child,
					WorkflowID: workflowID, State: "queued"}
				found = true
				return nil
			default:
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
		})
	return ticket, found, err
}

// ApplyAnalysis creates one legacy Apply for the final approved group set and
// commits the parent decision through the child admission transaction.
func (s Service) ApplyAnalysis(ctx context.Context, who auth.Identity,
	tenantID, analysisID uuid.UUID, key string, req ApplyDecision) (
	v2input.JobTicket, bool, error) {
	var empty v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	if s.DB == nil {
		return empty, false, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if analysisID == uuid.Nil || !idempotency.ValidKey(key) ||
		!digestPattern.MatchString(req.PlanSHA) || !refValid(req.Recipe) ||
		len(req.ApprovedGroups) < 1 || len(req.ApprovedGroups) > 5000 {
		return empty, false, domain.NewError("VALIDATION_FAILED", 422)
	}
	expected, err := revision(req.ExpectedRevision)
	if err != nil {
		return empty, false, err
	}
	decisionSHA, err := canonical.Hash(struct {
		AnalysisID uuid.UUID     `json:"analysis_id"`
		Decision   ApplyDecision `json:"decision"`
	}{analysisID, req})
	if err != nil {
		return empty, false, err
	}
	if ticket, replayed, err := s.analysisApplyReplay(ctx, who, tenantID,
		analysisID, key, decisionSHA); err != nil || replayed {
		if cancelled := ctx.Err(); cancelled != nil {
			return empty, false, cancelled
		}
		return ticket, replayed, err
	}
	if s.Objects == nil || s.Bucket == "" {
		return empty, false, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	state, err := s.analysisApplyContext(ctx, who, tenantID, analysisID, req.Recipe)
	if err != nil {
		return empty, false, err
	}
	reader := PlanMaterializer{Objects: s.Objects, Bucket: s.Bucket}
	planRaw, err := reader.readSceneObject(ctx, state.Plan)
	if err != nil {
		return empty, false, err
	}
	plan, err := validateApplyDecision(planRaw, req, state.SnapshotID,
		state.SnapshotDigest, state.PolicySHA, state.ItemCount)
	if err != nil {
		return empty, false, err
	}
	chosen, err := projectApplySelection(plan, req.ApprovedGroups)
	if err != nil {
		return empty, false, err
	}
	inputRaw, err := reader.readSceneObject(ctx, state.Input)
	if err != nil {
		return empty, false, err
	}
	bindings, err := validatedAnalysisBindings(inputRaw, state)
	if err != nil {
		return empty, false, err
	}
	groups := make(map[uuid.UUID]sceneGroup, len(plan.Groups))
	for _, group := range plan.Groups {
		groups[group.ID] = group
	}
	vectors := make(map[int]v2draft.CorrectionVector, len(chosen))
	seenGroup := make(map[uuid.UUID]bool, len(req.ApprovedGroups))
	for _, choice := range chosen {
		if seenGroup[choice.GroupID] {
			continue
		}
		seenGroup[choice.GroupID] = true
		group := groups[choice.GroupID]
		source, found := state.Corrections[group.Corrections.ArtifactID]
		if !found || source.Object.SHA256 != group.Corrections.SHA256 {
			return empty, false, domain.NewError("ANALYSIS_CONFLICT", 409)
		}
		raw, err := reader.readSceneObject(ctx, source)
		if err != nil {
			return empty, false, err
		}
		corrections, err := correctionVectors(raw, group,
			state.SnapshotDigest, bindings)
		if err != nil {
			return empty, false, err
		}
		for ordinal, vector := range corrections {
			vectors[ordinal] = vector
		}
	}
	itemIDs := make([]uuid.UUID, len(chosen))
	bound := make([]run.BoundItemOverride, len(chosen))
	for i, choice := range chosen {
		item := state.Items[choice.Ordinal]
		binding := bindings[choice.Ordinal]
		body, err := v2draft.BuildAnalysisOverride(state.StyleConfig,
			binding.Source.SHA256, vectors[choice.Ordinal])
		if err != nil {
			return empty, false, domain.NewError("PARAMETER_RANGE", 422)
		}
		itemIDs[i] = item.ItemID
		bound[i] = run.BoundItemOverride{ItemID: item.ItemID,
			AssetVersionID: item.AssetVersionID, InputSHA256: item.SHA256,
			Body: body}
	}
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	response, err := (run.Service{DB: s.DB}).ApplyWithCommit(ctx, who, tenantID,
		state.SourceCollection, key, run.ApplyRequest{
			SourceRevision: state.SourceRevision,
			StyleVersionID: state.StyleVersionID,
			Scope:          "selected", ItemIDs: itemIDs,
		}, decisionSHA, bound, func(ctx context.Context, tx pgx.Tx,
			accepted run.ApplyAccepted) error {
			var ok bool
			err := tx.QueryRow(ctx, `SELECT photo.finish_v22_analysis_apply(
		 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tenantID,
				analysisID, who.UserID, who.SessionID, expected, req.PlanSHA,
				req.Recipe.ArtifactID, req.Recipe.SHA256, decisionSHA,
				req.ApprovedGroups, accepted.RunID).Scan(&ok)
			if err != nil {
				return analysisError(err)
			}
			if !ok {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			return nil
		})
	if err != nil {
		return empty, false, analysisError(err)
	}
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	var accepted run.ApplyAccepted
	if json.Unmarshal(response.Body, &accepted) != nil || accepted.RunID == uuid.Nil {
		return empty, false, domain.NewError("INTERNAL_ERROR", 500)
	}
	return v2input.JobTicket{RunID: accepted.RunID,
		WorkflowID: state.WorkflowID, State: "queued"}, response.Replayed, nil
}
