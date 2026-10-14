package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// RelightBootstrapCoordinator prepares a complete blocked draft. Expected and
// budgets are deployment-controlled; no queue/publication authority is returned.
type RelightBootstrapCoordinator struct {
	DB                        DonorConfirmationTransactions
	Objects                   v2input.SessionAnalysisObjectReader
	Bucket                    string
	Expected                  v2qualification.Expected
	RasterLimits, BatchLimits RelightManualRasterLimits
}

type RelightBootstrapRequest struct {
	Who                                      auth.Identity
	Tenant, Project, Snapshot, Run, Workflow uuid.UUID
	Ordinal                                  int
	SubjectKind                              string
	ParamsRaw, SourceRaw                     []byte
	RuntimeSHA                               string
	Capacity                                 LocalResourceEstimate
	Pin                                      RelightQualificationPin
	Evidence                                 RelightQualificationEvidence
}

type RelightBootstrapResult struct {
	Intent    RelightIntent
	Documents RelightInputDocuments
	Producers RelightProducerDocuments
	prepared  *relightPreparedAuthority
}

func (s RelightBootstrapCoordinator) authority(ctx context.Context, tx pgx.Tx, r RelightBootstrapRequest, p RelightParams) error {
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", r.Tenant.String()); err != nil {
		return err
	}
	if err := LockRelightSource(ctx, tx, r.Who, r.Tenant, r.Project, r.Snapshot, p.Base); err != nil {
		return err
	}
	return LockRelightQualification(ctx, tx, r.Who, r.Tenant, r.Project, r.Pin)
}

func (s RelightBootstrapCoordinator) checkManual(ctx context.Context, tx pgx.Tx, r RelightBootstrapRequest, p RelightParams, producer RelightManualProducer) error {
	witness := producer.preintent
	if witness == nil {
		return v2input.ErrInvalid
	}
	selected := manualLocatorRef(witness.origin.Selected)
	origin, err := DiscoverRelightPreintentProducer(ctx, tx, r.Who, r.Tenant, r.Project, r.Snapshot, p.Base, selected, witness.selection, s.Bucket)
	if err != nil {
		return err
	}
	if origin != witness.origin {
		return v2input.ErrExpired
	}
	baseRaw, err := canonical.Marshal(p.Base)
	if err != nil {
		return err
	}
	selectedRaw, err := canonical.Marshal(selected)
	if err != nil {
		return err
	}
	for _, kind := range []string{"vectors", "target", "guard"} {
		expected, exists := witness.objects[kind]
		if !exists {
			continue
		}
		var raw []byte
		if err = tx.QueryRow(ctx, "SELECT photo.v28_relight_preintent_manual_object($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11)", r.Tenant, r.Project, r.Snapshot, r.Who.UserID, r.Who.SessionID, string(baseRaw), string(selectedRaw), witness.selection, witness.selectedBody, string(producer.RootBody), kind).Scan(&raw); err != nil {
			return err
		}
		normalized, e := canonical.Bytes(raw)
		if e != nil {
			return v2input.ErrExpired
		}
		pinned, e := canonical.Marshal(expected)
		if e != nil || !bytes.Equal(normalized, pinned) {
			return v2input.ErrExpired
		}
	}
	return nil
}

// Prepare loads and verifies all original groups, then retains current authority
// and exact object locators in one final transaction while saving the full intent.
// This creates no tasks, transport, reservation, outbox or admission receipt.
func (s RelightBootstrapCoordinator) Prepare(ctx context.Context, r RelightBootstrapRequest) (RelightBootstrapResult, error) {
	var zero RelightBootstrapResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || r.Tenant == uuid.Nil || r.Project == uuid.Nil || r.Snapshot == uuid.Nil || r.Who.UserID == uuid.Nil || r.Who.SessionID == uuid.Nil || s.RasterLimits.PointChecks < 0 || s.RasterLimits.EdgeEvaluations < 0 || s.RasterLimits.PayloadBytes < 1 || r.Run == uuid.Nil || r.Workflow == uuid.Nil || r.Ordinal < 0 || r.Ordinal >= 5000 || (r.SubjectKind != "person" && r.SubjectKind != "manual") || len(r.ParamsRaw) > 65536 || len(r.SourceRaw) > 1048576 || len(r.Evidence.Report) > 1048576 || len(r.Evidence.Calibration) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	r.ParamsRaw = append([]byte(nil), r.ParamsRaw...)
	r.SourceRaw = append([]byte(nil), r.SourceRaw...)
	r.Evidence.Report = append([]byte(nil), r.Evidence.Report...)
	r.Evidence.Calibration = append([]byte(nil), r.Evidence.Calibration...)
	budget, err := NewRelightManualRasterBudget(s.BatchLimits)
	if err != nil {
		return zero, err
	}
	var p RelightParams
	if json.Unmarshal(r.ParamsRaw, &p) != nil {
		return zero, v2input.ErrInvalid
	}
	plan, err := CompilePinnedRelightTaskPlan(ctx, p.Base, r.SourceRaw, r.ParamsRaw, r.RuntimeSHA, r.Capacity, r.Pin)
	if err != nil {
		return zero, err
	}
	if json.Unmarshal(plan.Params, &p) != nil {
		return zero, v2input.ErrInvalid
	}
	if err = VerifyRelightQualificationEvidence(ctx, p, r.Pin, r.Evidence.Report, r.Evidence.Calibration, s.Expected); err != nil {
		return zero, err
	}
	initial, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if err = s.authority(ctx, initial, r, p); err != nil {
		_ = initial.Rollback(context.Background())
		return zero, err
	}
	if err = initial.Commit(ctx); err != nil {
		_ = initial.Rollback(context.Background())
		return zero, err
	}
	reader := SQLRelightPreintentReader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}
	manualReader := SQLRelightPreintentManualReader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket, Limits: s.RasterLimits, Budget: budget}
	depth, err := reader.Load(ctx, r.Who, r.Tenant, r.Project, r.Snapshot, p, r.SourceRaw, "depth")
	if err != nil {
		return zero, err
	}
	documents := RelightInputDocuments{Source: r.SourceRaw, Depth: depth.SelectedBody}
	producers := RelightProducerDocuments{Depth: RelightDepthProducer{Ref: manualLocatorRef(depth.Origin.Params), Body: depth.ParamsBody}}
	var person RelightPreintentDocuments
	var manuals []RelightManualProducer
	if r.SubjectKind == "person" {
		person, err = reader.Load(ctx, r.Who, r.Tenant, r.Project, r.Snapshot, p, r.SourceRaw, "person")
		if err != nil {
			return zero, err
		}
		documents.Subject = person.SelectedBody
		producers.Person = &RelightPersonProducer{Ref: manualLocatorRef(person.Origin.Params), Body: person.ParamsBody}
	} else {
		target, e := manualReader.Load(ctx, r.Who, r.Tenant, r.Project, r.Snapshot, p, r.SourceRaw, false)
		if e != nil {
			return zero, e
		}
		manuals = append(manuals, target)
		documents.Subject = []byte(target.preintent.selectedBody)
	}
	guard, err := manualReader.Load(ctx, r.Who, r.Tenant, r.Project, r.Snapshot, p, r.SourceRaw, true)
	if err != nil {
		return zero, err
	}
	manuals = append(manuals, guard)
	documents.Protected = []byte(guard.preintent.selectedBody)
	for _, manual := range manuals {
		duplicate := false
		for _, old := range producers.Manual {
			if old.Root == manual.Root {
				if old.Original != manual.Original || !bytes.Equal(old.RootBody, manual.RootBody) || !bytes.Equal(old.ParamsBody, manual.ParamsBody) || !bytes.Equal(old.VectorsBody, manual.VectorsBody) {
					return zero, v2input.ErrInvalid
				}
				duplicate = true
			}
		}
		if !duplicate {
			producers.Manual = append(producers.Manual, manual)
		}
	}
	intent, err := CompileRelightProducerIntent(ctx, r.Workflow, r.Ordinal, r.Who, p.Base, documents, producers, r.ParamsRaw, r.RuntimeSHA, r.Capacity, r.Pin, r.Evidence, s.Expected)
	if err != nil {
		return zero, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	prepared := newRelightPreparedAuthority(s.Bucket, r, p, intent, depth.Origin, person.Origin, manuals, documents)
	result := RelightBootstrapResult{Intent: intent, Documents: documents, Producers: producers, prepared: prepared}
	if err = s.RevalidatePrepared(ctx, tx, result); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}
