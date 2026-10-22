package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// This is preparation, not admission: the returned real compiler closure must
// still be published/registered and pass the installed protected seal chain.
type AuthoredFacePreparation struct {
	Scheduler     *pgxpool.Pool
	Qualification SQLAuthoredFaceQualification
	Params        AuthoredFaceParamsPublication
	Producers     AuthoredFaceProducerReader
}

func (p AuthoredFacePreparation) PrepareDetector(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, score float64, maxFaces int, limits AuthoredFaceBudget) (v2input.FrozenSemanticDetectorTask, error) {
	var zero v2input.FrozenSemanticDetectorTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownAuthoredDefectExpected(expected)
	image = append(json.RawMessage(nil), image...)
	if who.UserID != source.Actor || who.SessionID != source.Session || p.Scheduler == nil || p.Params.Scheduler != p.Scheduler || expected.Operation != "face.detect" || expected.Version != "1" {
		return zero, v2input.ErrInvalid
	}
	proof, err := p.Qualification.Read(ctx, who, source.Tenant, source.Project, expected, report)
	if err != nil {
		return zero, err
	}
	prepared, err := v2input.PrepareSemanticFaceDetectParams(ctx, source.Source, image, expected.ModelLockSHA, score, maxFaces)
	if err != nil {
		return zero, err
	}
	_, workflow, err := AuthoredFacePrerequisiteIDs(source, "face.detect")
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, "face.detect", 0)
	if err != nil {
		return zero, err
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("params")), SHA256: authoredFaceCandidateSHA(prepared.Params)}
	sizes, err := readAuthoredFaceStageSizes(ctx, p.Scheduler, source.Tenant, prepared.Inputs, ref, int64(len(prepared.Params)))
	if err != nil {
		return zero, err
	}
	limits.Backend = expected.Backend
	limits.Costs = proof.costs
	limits.Sizes = sizes
	frozen, err := CompileAuthoredFaceDetector(ctx, source, image, ref, expected.ModelLockSHA, score, maxFaces, limits)
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if err = p.stageNative(ctx, who, source, expected, proof, draft, ref, prepared.Params, nil); err != nil {
		return zero, err
	}
	return frozen, ctx.Err()
}

func (p AuthoredFacePreparation) stageNative(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, expected v2qualification.Expected, proof AuthoredFaceQualification, draft v2input.DraftTask, ref v2input.Ref, paramsRaw []byte, detector *AuthoredFaceProducerObservation) error {
	var err error
	if err = p.Qualification.Recheck(ctx, who, source.Tenant, source.Project, expected, proof); err != nil {
		return err
	}
	pin := map[string]any{"contract": "QualificationReport", "artifact_id": proof.report.ArtifactID, "sha256": proof.report.SHA256, "bucket": p.Qualification.Bucket, "key": proof.object.Key, "version": proof.object.VersionID, "bytes": proof.object.Bytes, "backend": expected.Backend}
	marker := map[string]any{"ordinal": source.Instruction.Ordinal, "parent_run_id": source.Parent, "instruction_id": source.Instruction.Identity, "program_sha256": source.ProgramSHA, "predecessor_sha256": source.PredecessorSHA, "input_source": source.Source, "operation": expected.Operation, "operation_version": "1", "task_id": draft.ID, "params": ref, "params_jcs": string(paramsRaw), "input_digest": draft.InputDigest, "native_profile": draft.ExecutionProfile, "qualification_expected": expected, "qualification_report": pin}
	if detector != nil {
		marker["detector"] = *detector
	}
	var snapshot uuid.UUID
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT input_snapshot_id FROM photo.runs WHERE tenant_id=$1 AND id=$2", source.Tenant, source.Parent).Scan(&snapshot)
	})
	if err != nil {
		return err
	}
	if snapshot == uuid.Nil {
		return v2input.ErrInvalid
	}
	config, err := canonical.Marshal(map[string]any{"project_id": source.Project, "input_snapshot_id": snapshot, "authored_face": marker})
	if err != nil {
		return err
	}
	var staged bool
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.begin_authored_render_face_task($1,$2,$3,$4,$5,$6,$7::jsonb)", source.Tenant, source.Parent, source.Instruction.Ordinal, expected.Operation, ref.ArtifactID, string(config), draft.ExecutionProfile).Scan(&staged)
	})
	if err != nil {
		return err
	}
	if !staged {
		return v2input.ErrExpired
	}
	if _, err = p.Params.Publish(ctx, source, expected.Operation, ref, paramsRaw); err != nil {
		return err
	}
	if err = p.Qualification.Recheck(ctx, who, source.Tenant, source.Project, expected, proof); err != nil {
		return err
	}
	return ctx.Err()
}

func readAuthoredFaceStageSizes(ctx context.Context, db *pgxpool.Pool, tenant uuid.UUID, inputs []v2input.Entry, params v2input.Ref, n int64) ([]v2input.SemanticInputSize, error) {
	out := []v2input.SemanticInputSize{{Ref: params, Bytes: n}}
	seen := map[uuid.UUID]bool{params.ArtifactID: true}
	err := database.TenantTransaction(ctx, db, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		for _, entry := range inputs {
			if seen[entry.Ref.ArtifactID] {
				return v2input.ErrInvalid
			}
			seen[entry.Ref.ArtifactID] = true
			var size int64
			if err := tx.QueryRow(ctx, "SELECT bytes FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND sha256=$3 AND state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN ('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp())", tenant, entry.Ref.ArtifactID, entry.Ref.SHA256).Scan(&size); err != nil {
				return err
			}
			out = append(out, v2input.SemanticInputSize{Ref: entry.Ref, Bytes: size})
		}
		return nil
	})
	return out, err
}
