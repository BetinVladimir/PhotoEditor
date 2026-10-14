package provenance

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Historical reads use immutable task-input registration, never live admission
// permission. The closure walker proves each parent-child edge from pinned JSON.
type SQLSigningRenderClosureCatalog struct {
	DB      *pgxpool.Pool
	Bucket  string
	Tasks   SigningTaskCatalog
	Ready   ReadySigningExport
	Task    CanonicalSigningTask
	Binding v2input.SessionVersionBinding
}

func (s SQLSigningRenderClosureCatalog) Lookup(ctx context.Context, who auth.Identity, tid uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tasks == nil || len(s.Bucket) < 3 || tid != s.Ready.Tenant || !ref(node.Ref) || node.Contract == "" || s.Task.Operation != "render.compose" || s.Task.Version != "1" {
		return zero, ErrInvalid
	}
	ownedTask, ownedNode, expected, err := ownSigningRenderCatalogSnapshot(s.Task, node)
	if err != nil {
		return zero, err
	}
	s.Task, node = ownedTask, ownedNode
	if !validSigningRenderOccurrence(node) {
		return zero, ErrInvalid
	}
	check := func() error {
		current, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, s.Ready, s.Task.Task)
		if err != nil {
			return err
		}
		if normalizeCanonicalSigningTask(&current) != nil {
			return ErrInvalid
		}
		raw, err := canonical.Marshal(current)
		if err != nil || !bytes.Equal(raw, expected) {
			return ErrInvalid
		}
		return nil
	}
	if err := check(); err != nil {
		return zero, err
	}
	contract := node.Contract
	switch contract {
	case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
		contract = "NpyArray"
	}
	if node.Contract == "FrozenRenderRecipe" && (node.Ref != s.Task.Params || node.ParentRef.ArtifactID != uuid.Nil) {
		return zero, ErrInvalid
	}
	if node.ParentRef.ArtifactID != uuid.Nil && !ref(node.ParentRef) {
		return zero, ErrInvalid
	}
	var sourceVersion any
	if node.Contract == "WorkingImage" {
		if node.ExpectedBinding != nil {
			sourceVersion = node.ExpectedBinding.AssetVersionID
		} else if node.Ref == s.Binding.Source {
			sourceVersion = s.Binding.AssetVersionID
		}
	}
	var object objectstore.Object
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, tid, s.Ready.Project, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		if node.Contract == "StyleV2" {
			var bound bool
			if err := tx.QueryRow(ctx, signingHistoricalStyleBindingSQL, tid, s.Ready.Project, node.Ref.ArtifactID, node.Ref.SHA256).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return ErrInvalid
			}
		}
		if node.Contract == "CandidateProfile" || node.Contract == "DescriptorShard" {
			learning := node.ExpectedLearning
			var bound bool
			if err := tx.QueryRow(ctx, signingHistoricalLearningBindingSQL, tid, s.Ready.Project, learning.VersionID, learning.Model.ArtifactID, learning.Model.SHA256, s.Task.Task).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return ErrInvalid
			}
		}
		if node.Contract == "DescriptorShard" {
			learning, binding := node.ExpectedLearning, node.ExpectedBinding
			var bound bool
			if err := tx.QueryRow(ctx, signingHistoricalDescriptorBindingSQL, tid, s.Ready.Project, learning.AnalysisID, learning.Ordinal, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, node.Ref.ArtifactID, node.Ref.SHA256, node.ExpectedModelLockSHA).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return ErrInvalid
			}
		}
		if node.RenderFrame != nil {
			frame, binding := node.RenderFrame, node.ExpectedBinding
			if frame.Root != s.Task.Params || frame.SelectedVersion != s.Binding.AssetVersionID {
				return ErrInvalid
			}
			var bound bool
			if err := tx.QueryRow(ctx, signingHistoricalDescriptorBindingSQL, tid, s.Ready.Project, frame.Learning.AnalysisID, frame.Learning.Ordinal, s.Binding.AssetVersionID, s.Binding.Source.ArtifactID, s.Binding.Source.SHA256, frame.Descriptor.ArtifactID, frame.Descriptor.SHA256, frame.ModelLockSHA).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return ErrInvalid
			}
			if err := tx.QueryRow(ctx, signingHistoricalFrameBindingSQL, tid, s.Ready.Project, frame.Learning.AnalysisID, frame.Ordinal, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return ErrInvalid
			}
		}
		if node.Contract == "StyleBackgroundAsset" {
			pin := node.ExpectedBackground
			var bound bool
			if err := tx.QueryRow(ctx, signingHistoricalBackgroundBindingSQL, tid, s.Ready.Project, pin.StyleVersionID, pin.Style.ArtifactID, pin.Style.SHA256, pin.Source.ArtifactID, pin.Source.SHA256, pin.PortableAssetID, who.UserID, who.SessionID, pin.ExpectedBytes, pin.MediaType).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return ErrInvalid
			}
		}
		err := tx.QueryRow(ctx, `SELECT a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
   FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.project_id=i.project_id AND w.run_id=$4
   WHERE i.tenant_id=$1 AND i.task_id=$2 AND i.project_id=$3 AND i.artifact_id=$5 AND i.sha256=$6
   AND (i.contract=$7 OR $7='Artifact') AND i.contract NOT IN('InputClosureManifest','InputClosureChunk')
   AND a.bucket=$8 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND ($9::uuid IS NULL OR EXISTS(SELECT 1 FROM photo.v2_task_inputs parent
    WHERE parent.tenant_id=i.tenant_id AND parent.task_id=i.task_id AND parent.project_id=i.project_id
    AND parent.artifact_id=$9 AND parent.sha256=$10))
   AND ($7<>'WorkingImage' OR EXISTS(SELECT 1 FROM photo.asset_versions version
    JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
    WHERE version.tenant_id=i.tenant_id AND asset.project_id=i.project_id
    AND version.master_artifact_id=a.id AND version.content_hash=a.sha256 AND version.review_state='accepted'
    AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND ($11::uuid IS NULL OR version.id=$11)))
   FOR SHARE OF a`, tid, s.Task.Task, s.Ready.Project, s.Task.Run, node.Ref.ArtifactID, node.Ref.SHA256, contract, s.Bucket, nullSigningParentID(node), node.ParentRef.SHA256, sourceVersion).Scan(&object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
		if err != nil {
			return err
		}
		_, err = project.Authorize(ctx, tx, tid, s.Ready.Project, who.UserID, role, domain.Editor, false)
		return err
	})
	if err != nil {
		return zero, err
	}
	if object.Key == "" {
		return zero, ErrInvalid
	}
	if err := check(); err != nil {
		return zero, err
	}
	return object, ctx.Err()
}
func nullSigningParentID(node v2draft.LayerRef) any {
	if node.ParentRef.ArtifactID == uuid.Nil {
		return nil
	}
	return node.ParentRef.ArtifactID
}

var _ v2draft.ArtifactCatalog = SQLSigningRenderClosureCatalog{}

type SQLSigningRenderClosureReader struct {
	DB      *pgxpool.Pool
	Bucket  string
	Tasks   SigningTaskCatalog
	Objects SigningObjects
}

func (s SQLSigningRenderClosureReader) ReadSigningRenderClosure(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (SigningRenderClosureEvidence, error) {
	var zero SigningRenderClosureEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tasks == nil || s.Objects == nil || len(s.Bucket) < 3 {
		return zero, ErrInvalid
	}
	adoption := SQLSigningRenderAdoptionReader{DB: s.DB, Tasks: s.Tasks}
	root, err := adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	documents := BoundSigningTaskDocumentReader{Catalog: SQLSigningTaskDocumentCatalog{DB: s.DB, Bucket: s.Bucket, Tasks: s.Tasks}, Objects: s.Objects}
	recipe, err := ReadSigningRenderRecipe(ctx, documents, who, ready, task)
	if err != nil {
		return zero, err
	}
	catalog := SQLSigningRenderClosureCatalog{DB: s.DB, Bucket: s.Bucket, Tasks: s.Tasks, Ready: ready, Task: task, Binding: recipe.Scalars.Source}
	closure := v2draft.ClosureResolver{Catalog: catalog, Objects: s.Objects}
	evidence, err := (BoundSigningRenderClosureSource{Documents: documents, Closure: closure}).ReadSigningRenderClosure(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	if err := verifySigningRenderDocumentCoverage(evidence); err != nil {
		return zero, err
	}
	if err := verifySigningRenderBinaryBytes(ctx, catalog, s.Objects, who, ready, evidence); err != nil {
		return zero, err
	}
	if err := verifySigningRenderBackgroundBytes(ctx, catalog, s.Objects, who, ready, evidence, recipe.Scalars.ICCLockSHA); err != nil {
		return zero, err
	}
	if err := verifySigningRenderModelOrigins(ctx, sqlSigningRenderModelOriginReader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}, who, ready, task, evidence, recipe.Scalars.ModelLockSHA, recipe.Scalars.ICCLockSHA); err != nil {
		return zero, err
	}
	current, err := adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	if current != root {
		return zero, ErrInvalid
	}
	return evidence, ctx.Err()
}

// Match the occurrence evidence required by render admission without granting
// admission or assuming an historical learning version is currently active.
func validSigningRenderOccurrence(node v2draft.LayerRef) bool {
	if !ref(node.Ref) {
		return false
	}
	if node.ParentRef.ArtifactID == uuid.Nil && node.ParentRef.SHA256 != "" {
		return false
	}
	if node.ParentRef.ArtifactID != uuid.Nil && !ref(node.ParentRef) {
		return false
	}
	switch node.Contract {
	case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
		if !ref(node.ParentRef) || node.ParentRef.ArtifactID == node.Ref.ArtifactID {
			return false
		}
	}
	if node.RenderFrame != nil {
		frame, binding := node.RenderFrame, node.ExpectedBinding
		if binding == nil || !ref(binding.Source) || binding.AssetVersionID == uuid.Nil || !digest(binding.CanvasHash) || !ref(frame.Root) || !ref(frame.Descriptor) || frame.SelectedVersion == uuid.Nil || frame.Learning.Descriptor != frame.Descriptor || frame.Learning.AnalysisID == uuid.Nil || frame.Ordinal < 0 || frame.Ordinal > 4999 || !digest(frame.ModelLockSHA) {
			return false
		}
		if (node.Contract == "WorkingPixels" || node.Contract == "WorkingAlpha") && node.ParentRef != binding.Source {
			return false
		}
		if node.Contract == "WorkingImage" && (node.Ref != binding.Source || node.CanvasHash != binding.CanvasHash || !ref(node.ParentRef)) {
			return false
		}
	}
	switch node.Contract {
	case "CandidateProfile", "DescriptorShard":
		learning, binding := node.ExpectedLearning, node.ExpectedBinding
		if learning == nil || binding == nil || !ref(node.ParentRef) || learning.VersionID == uuid.Nil || learning.AnalysisID == uuid.Nil || learning.Ordinal < 0 || learning.Ordinal > 4999 || binding.AssetVersionID == uuid.Nil || !ref(binding.Source) || !digest(binding.CanvasHash) || !ref(learning.Model) || !ref(learning.Descriptor) {
			return false
		}
		if node.Contract == "CandidateProfile" {
			return learning.Model == node.Ref
		}
		return learning.Descriptor == node.Ref && digest(node.ExpectedModelLockSHA)
	case "StyleBackgroundAsset":
		pin := node.ExpectedBackground
		if pin == nil || pin.Source != node.Ref || !ref(node.ParentRef) {
			return false
		}
		raw, err := canonical.Marshal(pin)
		if err != nil {
			return false
		}
		_, err = v2draft.DecodeFrozenBackgroundAsset(raw)
		return err == nil
	default:
		return true
	}
}

// Historical identity binding deliberately does not require active_version_id.
// Consent eligibility is a separate policy gate, not inferred from this query.
// Historical attribution is independent of the current active profile pointer
// and consent epoch. New model execution must use its separate live-consent gate.
// Exact canonical fit/adoption fencing is still mandatory for this model pin.
const signingHistoricalLearningBindingSQL = `SELECT EXISTS(
 SELECT 1 FROM photo.learning_versions version
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=version.tenant_id
  AND workflow.project_id=version.project_id AND workflow.id=version.workflow_id
 JOIN photo.runs run ON run.tenant_id=workflow.tenant_id AND run.id=workflow.run_id
 JOIN photo.artifacts model ON model.tenant_id=version.tenant_id AND model.id=version.model_artifact_id
 JOIN photo.tasks fit ON fit.tenant_id=model.tenant_id AND fit.id=model.created_by_task_id AND fit.run_id=run.id
 JOIN photo.task_results result ON result.tenant_id=fit.tenant_id AND result.task_id=fit.id AND result.id=fit.canonical_result_id
 JOIN photo.artifacts training ON training.tenant_id=version.tenant_id AND training.id=version.training_artifact_id
 JOIN photo.snapshot_items training_item ON training_item.tenant_id=run.tenant_id AND training_item.snapshot_id=run.input_snapshot_id
  AND training_item.ordinal=0 AND training_item.artifact_id=training.id AND training_item.sha256=training.sha256
 JOIN photo.artifacts evaluation ON evaluation.tenant_id=version.tenant_id AND evaluation.id=version.evaluation_artifact_id
 JOIN photo.tasks evaluated ON evaluated.tenant_id=evaluation.tenant_id AND evaluated.id=evaluation.created_by_task_id AND evaluated.run_id=run.id
 JOIN photo.task_results evaluated_result ON evaluated_result.tenant_id=evaluated.tenant_id
  AND evaluated_result.task_id=evaluated.id AND evaluated_result.id=evaluated.canonical_result_id
 JOIN photo.v2_task_inputs input ON input.tenant_id=version.tenant_id AND input.project_id=version.project_id
  AND input.artifact_id=model.id AND input.sha256=model.sha256
 WHERE version.tenant_id=$1 AND version.project_id=$2 AND version.id=$3
 AND model.id=$4 AND model.sha256=$5 AND input.task_id=$6 AND input.contract='CandidateProfile'
 AND workflow.workflow_type='learning' AND workflow.renderer_version='v2.2'
 AND run.kind='v2' AND run.graph_sealed AND workflow.input_snapshot_id=run.input_snapshot_id
 AND fit.operation='learning.fit' AND fit.operation_version='1' AND fit.state='succeeded'
 AND result.attempt=fit.attempt AND result.fencing_token=fit.fencing_token
 AND model.produced_attempt=fit.attempt AND model.produced_fence=fit.fencing_token AND model.output_role='document'
 AND result.outputs=jsonb_build_array(jsonb_build_object('artifact_id',model.id::text,'sha256',model.sha256))
 AND EXISTS(SELECT 1 FROM photo.inbox adopted WHERE adopted.tenant_id=fit.tenant_id
  AND adopted.consumer='v2-adopt' AND adopted.event_id=photo.v2_adoption_event_id(fit.id,fit.canonical_result_id))
 AND training.sha256=run.config_snapshot->'v23_training'->>'manifest_sha'
 AND training.state='ready' AND training.verified_at IS NOT NULL AND training.media_type='application/json'
 AND training.object_version NOT IN('','null') AND (training.expires_at IS NULL OR training.expires_at>clock_timestamp())
 AND evaluated.operation='learning.evaluate' AND evaluated.operation_version='1' AND evaluated.state='succeeded'
 AND evaluated_result.attempt=evaluated.attempt AND evaluated_result.fencing_token=evaluated.fencing_token
 AND evaluation.produced_attempt=evaluated.attempt AND evaluation.produced_fence=evaluated.fencing_token
 AND evaluation.output_role='document' AND evaluation.state='ready' AND evaluation.verified_at IS NOT NULL
 AND evaluation.media_type='application/json' AND evaluation.object_version NOT IN('','null')
 AND (evaluation.expires_at IS NULL OR evaluation.expires_at>clock_timestamp())
 AND evaluated_result.outputs=jsonb_build_array(jsonb_build_object('artifact_id',evaluation.id::text,'sha256',evaluation.sha256))
 AND EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=run.tenant_id AND edge.run_id=run.id
  AND edge.prerequisite_id=fit.id AND edge.dependent_id=evaluated.id AND edge.condition='success')
 AND EXISTS(SELECT 1 FROM photo.inbox adopted WHERE adopted.tenant_id=evaluated.tenant_id
  AND adopted.consumer='v2-adopt' AND adopted.event_id=photo.v2_adoption_event_id(evaluated.id,evaluated.canonical_result_id))
 AND model.state='ready' AND model.verified_at IS NOT NULL AND model.media_type='application/json'
 AND model.object_version NOT IN('','null') AND (model.expires_at IS NULL OR model.expires_at>clock_timestamp()))`

const signingHistoricalDescriptorBindingSQL = `SELECT EXISTS(
 SELECT 1 FROM photo.session_analyses analysis
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=analysis.tenant_id AND workflow.project_id=analysis.project_id AND workflow.id=analysis.workflow_id
 JOIN photo.runs run ON run.tenant_id=workflow.tenant_id AND run.id=workflow.run_id
 JOIN photo.snapshot_items item ON item.tenant_id=analysis.tenant_id AND item.snapshot_id=analysis.snapshot_id
 JOIN photo.tasks producer ON producer.tenant_id=run.tenant_id AND producer.run_id=run.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id AND result.id=producer.canonical_result_id
 JOIN photo.artifacts descriptor ON descriptor.tenant_id=producer.tenant_id AND descriptor.id=$8 AND descriptor.sha256=$9
 JOIN photo.artifacts plan ON plan.tenant_id=analysis.tenant_id AND plan.id=analysis.plan_artifact_id
 WHERE analysis.tenant_id=$1 AND analysis.project_id=$2 AND analysis.id=$3 AND analysis.model_lock_sha=$10
 AND item.ordinal=$4 AND item.asset_version_id=$5 AND item.artifact_id=$6 AND item.sha256=$7
 AND workflow.workflow_type='session' AND workflow.renderer_version='v2.2'
 AND run.kind='v2' AND run.graph_sealed AND NOT run.cancel_requested AND run.status IN('waiting_review','completed')
 AND producer.operation='session.describe' AND producer.operation_version='1' AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 AND result.outputs=jsonb_build_array(jsonb_build_object('artifact_id',descriptor.id::text,'sha256',descriptor.sha256))
 AND descriptor.created_by_task_id=producer.id AND descriptor.produced_attempt=producer.attempt AND descriptor.produced_fence=producer.fencing_token
 AND descriptor.state='ready' AND descriptor.verified_at IS NOT NULL AND descriptor.media_type='application/json' AND descriptor.output_role='document'
 AND descriptor.object_version NOT IN('','null') AND (descriptor.expires_at IS NULL OR descriptor.expires_at>clock_timestamp())
 AND plan.state='ready' AND plan.verified_at IS NOT NULL AND plan.object_version NOT IN('','null')
 AND (plan.expires_at IS NULL OR plan.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.scene_groups scene JOIN photo.scene_members member
 ON member.tenant_id=scene.tenant_id AND member.project_id=scene.project_id AND member.group_id=scene.id
 WHERE scene.tenant_id=analysis.tenant_id AND scene.project_id=analysis.project_id AND scene.analysis_id=analysis.id
 AND member.asset_version_id=$5 AND member.snapshot_ordinal=$4))`

// Version/config/binding are historical pins; no current default style lookup.
const signingHistoricalBackgroundBindingSQL = `SELECT EXISTS(
 SELECT 1 FROM photo.styles style
 JOIN photo.style_versions version ON version.tenant_id=style.tenant_id AND version.style_id=style.id
 JOIN photo.style_asset_bindings binding ON binding.tenant_id=version.tenant_id AND binding.style_version_id=version.id
 JOIN photo.artifacts asset ON asset.tenant_id=binding.tenant_id AND asset.id=binding.artifact_id AND asset.sha256=binding.sha256
 JOIN photo.artifacts config ON config.tenant_id=version.tenant_id AND config.id=version.export_artifact_id AND config.sha256=version.config_hash
 WHERE style.tenant_id=$1 AND style.project_id=$2 AND version.id=$3
 AND version.export_artifact_id=$4 AND version.config_hash=$5 AND version.approval_status='accepted'
 AND binding.artifact_id=$6 AND binding.sha256=$7 AND binding.portable_asset_id=$8
 AND version.config_json#>>'{extensions,background,mode}'='asset'
 AND version.config_json#>>'{extensions,background,asset_id}'=$8
 AND (asset.output_role='style_asset' OR (asset.output_role='v30_import_prepared'
  AND photo.v30_registered_style_background($1,$2,version.id,asset.id,asset.sha256,$8,$9,$10) IS TRUE))
 AND asset.bytes=$11 AND asset.media_type=$12 AND asset.media_type IN('image/jpeg','image/png','image/tiff')
 AND asset.state='ready' AND asset.verified_at IS NOT NULL AND asset.object_version NOT IN('','null')
 AND (asset.expires_at IS NULL OR asset.expires_at>clock_timestamp())
 AND config.state='ready' AND config.verified_at IS NOT NULL AND config.object_version NOT IN('','null')
 AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp()))`

const signingHistoricalFrameBindingSQL = `SELECT EXISTS(
 SELECT 1 FROM photo.session_analyses analysis
 JOIN photo.snapshot_items item ON item.tenant_id=analysis.tenant_id AND item.snapshot_id=analysis.snapshot_id
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id AND version.id=item.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.artifacts source ON source.tenant_id=version.tenant_id AND source.id=version.master_artifact_id
 WHERE analysis.tenant_id=$1 AND analysis.project_id=$2 AND analysis.id=$3 AND $4 BETWEEN 0 AND 4999
 AND item.ordinal=$4 AND item.asset_version_id=$5 AND item.artifact_id=$6 AND item.sha256=$7
 AND version.master_artifact_id=$6 AND version.content_hash=$7 AND version.review_state='accepted'
 AND asset.project_id=$2 AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND source.sha256=$7 AND source.output_role='working_image' AND source.media_type='application/json'
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.scene_groups scene JOIN photo.scene_members member
 ON member.tenant_id=scene.tenant_id AND member.project_id=scene.project_id AND member.group_id=scene.id
 WHERE scene.tenant_id=analysis.tenant_id AND scene.project_id=analysis.project_id AND scene.analysis_id=analysis.id
 AND member.asset_version_id=$5 AND member.snapshot_ordinal=$4))`

// Always unmarshal into zero values: unmarshalling into an existing struct can
// reuse its RawMessage backing arrays and nested pointer fields.
func ownSigningRenderCatalogSnapshot(task CanonicalSigningTask, node v2draft.LayerRef) (CanonicalSigningTask, v2draft.LayerRef, []byte, error) {
	var ownedTask CanonicalSigningTask
	var ownedNode v2draft.LayerRef
	taskBytes, err := canonical.Marshal(task)
	if err != nil || len(taskBytes) > 64<<20 {
		return ownedTask, ownedNode, nil, ErrInvalid
	}
	nodeBytes, err := canonical.Marshal(node)
	if err != nil || len(nodeBytes) > 1048576 {
		return ownedTask, ownedNode, nil, ErrInvalid
	}
	if json.Unmarshal(taskBytes, &ownedTask) != nil || json.Unmarshal(nodeBytes, &ownedNode) != nil {
		return CanonicalSigningTask{}, v2draft.LayerRef{}, nil, ErrInvalid
	}
	return ownedTask, ownedNode, taskBytes, nil
}

// Historical exact style version remains attributable after default changes or
// archival. The ready task inventory and object reader are separate gates.
const signingHistoricalStyleBindingSQL = `SELECT EXISTS(
 SELECT 1 FROM photo.style_versions version JOIN photo.styles style
 ON style.tenant_id=version.tenant_id AND style.id=version.style_id
 WHERE version.tenant_id=$1 AND style.project_id=$2
 AND version.export_artifact_id=$3 AND version.config_hash=$4
 AND version.approval_status='accepted' AND version.schema_version='2.0.0'
 AND version.renderer_version='photo_renderer_v2')`
