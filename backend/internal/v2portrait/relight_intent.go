package v2portrait

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type RelightIntent struct {
	ActorID       uuid.UUID               `json:"actor_id"`
	AuthSessionID uuid.UUID               `json:"auth_session_id"`
	NodeKey       string                  `json:"node_key"`
	OwnerID       uuid.UUID               `json:"owner_id"`
	ParamsJCS     string                  `json:"params_jcs"`
	ParamsSHA     string                  `json:"params_sha"`
	ProfileJCS    string                  `json:"profile_jcs"`
	ProfileSHA    string                  `json:"profile_sha"`
	InputsJCS     string                  `json:"inputs_jcs"`
	InputsSHA     string                  `json:"inputs_sha"`
	ReservedBytes int64                   `json:"reserved_bytes"`
	Qualification RelightQualificationPin `json:"qualification"`
}

// CompileRelightIntent freezes validated direct inputs and evidence. The SQL
// save gate must recheck current source/qualification; producer closure is still
// required before any publication/admission grant.
func CompileRelightIntent(ctx context.Context, workflow uuid.UUID, ordinal int, who auth.Identity,
	binding v2input.SessionVersionBinding, documents RelightInputDocuments, paramsRaw []byte,
	runtimeSHA string, capacity LocalResourceEstimate, pin RelightQualificationPin,
	evidence RelightQualificationEvidence, expected v2qualification.Expected) (RelightIntent, error) {
	return compileRelightIntent(ctx, workflow, ordinal, who, binding, documents, nil, paramsRaw, runtimeSHA, capacity, pin, evidence, expected)
}

// CompileRelightProducerIntent freezes content-validated original producer
// closure. It does not substitute for live SQL producer authority or pixel replay.
func CompileRelightProducerIntent(ctx context.Context, workflow uuid.UUID, ordinal int, who auth.Identity,
	binding v2input.SessionVersionBinding, documents RelightInputDocuments, producers RelightProducerDocuments, paramsRaw []byte,
	runtimeSHA string, capacity LocalResourceEstimate, pin RelightQualificationPin,
	evidence RelightQualificationEvidence, expected v2qualification.Expected) (RelightIntent, error) {
	return compileRelightIntent(ctx, workflow, ordinal, who, binding, documents, &producers, paramsRaw, runtimeSHA, capacity, pin, evidence, expected)
}

func compileRelightIntent(ctx context.Context, workflow uuid.UUID, ordinal int, who auth.Identity,
	binding v2input.SessionVersionBinding, documents RelightInputDocuments, producers *RelightProducerDocuments, paramsRaw []byte,
	runtimeSHA string, capacity LocalResourceEstimate, pin RelightQualificationPin,
	evidence RelightQualificationEvidence, expected v2qualification.Expected) (RelightIntent, error) {
	var zero RelightIntent
	if workflow == uuid.Nil || ordinal < 0 || ordinal >= 5000 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var compiled RelightInputPlan
	var err error
	if producers == nil {
		compiled, err = CompileRelightInputPlan(ctx, binding, documents, paramsRaw, runtimeSHA, capacity, pin)
	} else {
		compiled, err = CompileRelightFullInputPlan(ctx, binding, documents, *producers, paramsRaw, runtimeSHA, capacity, pin)
	}
	if err != nil {
		return zero, err
	}
	var params RelightParams
	if json.Unmarshal(compiled.Plan.Params, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	if err = VerifyRelightQualificationEvidence(ctx, params, pin, evidence.Report, evidence.Calibration, expected); err != nil {
		return zero, err
	}
	inputs, err := canonical.Marshal(compiled.Inputs)
	if err != nil {
		return zero, err
	}
	profileSHA, err := canonical.Hash(json.RawMessage(compiled.Plan.Profile))
	if err != nil {
		return zero, err
	}
	inputsSHA, err := canonical.Hash(compiled.Inputs)
	if err != nil {
		return zero, err
	}
	if len(inputs) > 65536 || len(compiled.Plan.Profile) > 65536 {
		return zero, v2input.ErrInvalid
	}
	node := fmt.Sprintf("image.relight:%d", ordinal)
	return RelightIntent{ActorID: who.UserID, AuthSessionID: who.SessionID, NodeKey: node, OwnerID: uuid.NewSHA1(workflow, []byte(node)), ParamsJCS: string(compiled.Plan.Params), ParamsSHA: compiled.Plan.ParamsSHA, ProfileJCS: string(compiled.Plan.Profile), ProfileSHA: profileSHA, InputsJCS: string(inputs), InputsSHA: inputsSHA, ReservedBytes: compiled.Plan.ReservedBytes, Qualification: pin}, ctx.Err()
}

func SaveRelightIntent(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID, intent RelightIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(intent)
	if err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.save_v28_relight_intent($1,$2,$3::jsonb)", tenant, run, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
