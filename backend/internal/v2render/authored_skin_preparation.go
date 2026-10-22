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

// The existing worker emits a full-image skin probability mask. The private
// persisted face decision authorizes its use in this current authored phase;
// it does not turn that output into a selected-object-only label map.
type AuthoredSkinPreparation struct {
	Scheduler     *pgxpool.Pool
	Qualification SQLAuthoredSkinQualification
	Params        AuthoredSkinParamsPublication
	Producers     AuthoredFaceProducerReader
}

func AuthoredSkinPrerequisiteIDs(source AuthoredInstructionSource, operation string) (uuid.UUID, uuid.UUID, error) {
	if operation != "mask.face_skin" {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	if _, _, err := AuthoredFacePrerequisiteIDs(source, "face.parse"); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	run := uuid.NewSHA1(source.Instruction.Identity, []byte("AuthoredRepairFace/v1:mask.face_skin"))
	workflow, err := v2input.WorkflowID(run)
	return run, workflow, err
}

// Prepare consumes only the private SQL-read current decision. Callers supply no
// face IDs, parser roots, profiles or fabricated qualification observations.
func (p AuthoredSkinPreparation) Prepare(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, selection CurrentAuthoredFaceSelection, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) (v2input.FrozenSemanticFaceSkinTask, error) {
	var zero v2input.FrozenSemanticFaceSkinTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Params.Scheduler != p.Scheduler || p.Producers.Scheduler != p.Scheduler || who.UserID != source.Actor || who.SessionID != source.Session || expected.Operation != "mask.face_skin" || expected.Version != "1" || expected.Backend != "cpu" || limits.Backend != "cpu" || limits.VRAM != 0 {
		return zero, v2input.ErrInvalid
	}
	expected = ownAuthoredDefectExpected(expected)
	image = append(json.RawMessage(nil), image...)
	if err := RecheckCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, selection); err != nil {
		return zero, err
	}
	proof, err := p.Qualification.read(ctx, who, source, expected, report)
	if err != nil {
		return zero, err
	}
	detector, err := p.Producers.read(ctx, source, "face.detect")
	if err != nil {
		return zero, err
	}
	parser, err := p.Producers.read(ctx, source, "face.parse")
	if err != nil {
		return zero, err
	}
	// The immutable candidate must be the one whose exact producer vectors were
	// independently reacquired now. Neither accepted old IDs nor a fresh warning
	// observation can be substituted into another decision.
	if !authoredSkinProducerEqual(selection.candidate.Detector, detector.observation) || !authoredSkinProducerEqual(selection.candidate.Parser, parser.observation) {
		return zero, v2input.ErrExpired
	}
	d, r := detector.observation, parser.observation
	prepared, err := v2input.PrepareSemanticFaceSkinParams(ctx, source.Source, image, r.Roots[1], parser.root, r.Params, parser.params, d.Roots[0], detector.root)
	if err != nil {
		return zero, err
	}
	_, workflow, err := AuthoredSkinPrerequisiteIDs(source, "mask.face_skin")
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, "mask.face_skin", 0)
	if err != nil {
		return zero, err
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("params")), SHA256: authoredFaceCandidateSHA(prepared.Params)}
	sizes, err := readAuthoredFaceStageSizes(ctx, p.Scheduler, source.Tenant, prepared.Inputs, ref, int64(len(prepared.Params)))
	if err != nil {
		return zero, err
	}
	limits.Sizes = sizes
	frozen, err := CompileAuthoredFaceSkin(ctx, source, image, r.Roots[1], parser.root, r.Params, parser.params, d.Roots[0], detector.root, ref, limits)
	if err != nil {
		return zero, err
	}
	frozen, err = frozen.WithInputIndexScratch()
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if err = authoredSkinProfileWithinLimits(draft.ExecutionProfile, limits); err != nil {
		return zero, err
	}
	if err = authoredSkinMeasuredBudget(draft.ExecutionProfile, proof.report); err != nil {
		return zero, err
	}
	if err = p.stage(ctx, who, source, selection, expected, proof, draft, ref, prepared.Params, d, r); err != nil {
		return zero, err
	}
	return frozen, ctx.Err()
}
func authoredSkinProducerEqual(a, b AuthoredFaceProducerObservation) bool {
	left, e := canonical.Marshal(a)
	if e != nil {
		return false
	}
	right, e := canonical.Marshal(b)
	return e == nil && string(left) == string(right)
}

func (p AuthoredSkinPreparation) stage(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, selection CurrentAuthoredFaceSelection, expected v2qualification.Expected, proof authoredSkinQualification, draft v2input.DraftTask, ref v2input.Ref, params []byte, detector, parser AuthoredFaceProducerObservation) error {
	if err := RecheckCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, selection); err != nil {
		return err
	}
	fresh, err := p.Qualification.read(ctx, who, source, expected, proof.ref)
	if err != nil {
		return err
	}
	if fresh.expected != proof.expected || fresh.object != proof.object || string(fresh.raw) != string(proof.raw) {
		return v2input.ErrExpired
	}
	pin := map[string]any{"contract": "QualificationReport", "artifact_id": proof.ref.ArtifactID, "sha256": proof.ref.SHA256, "bucket": p.Qualification.Bucket, "key": proof.object.Key, "version": proof.object.VersionID, "bytes": proof.object.Bytes, "backend": "cpu"}
	marker := map[string]any{"ordinal": source.Instruction.Ordinal, "parent_run_id": source.Parent, "instruction_id": source.Instruction.Identity, "program_sha256": source.ProgramSHA, "predecessor_sha256": source.PredecessorSHA, "input_source": source.Source, "operation": "mask.face_skin", "operation_version": "1", "task_id": draft.ID, "params": ref, "params_jcs": string(params), "input_digest": draft.InputDigest, "native_profile": draft.ExecutionProfile, "qualification_expected": expected, "qualification_report": pin, "qualification_jcs": string(proof.raw), "candidate_sha256": selection.sha, "selection_jcs": selection.proof, "detector": detector, "parser": parser}
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
	config, err := canonical.Marshal(map[string]any{"project_id": source.Project, "input_snapshot_id": snapshot, "authored_skin": marker})
	if err != nil {
		return err
	}
	var staged bool
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.begin_authored_render_skin_task($1,$2,$3,$4,$5,$6,$7::jsonb)", source.Tenant, source.Parent, source.Instruction.Ordinal, "mask.face_skin", ref.ArtifactID, string(config), draft.ExecutionProfile).Scan(&staged)
	})
	if err != nil {
		return err
	}
	if !staged {
		return v2input.ErrExpired
	}
	if _, err = p.Params.Publish(ctx, source, "mask.face_skin", ref, params); err != nil {
		return err
	}
	return RecheckCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, selection)
}
