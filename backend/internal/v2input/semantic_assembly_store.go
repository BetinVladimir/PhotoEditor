package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/canonical"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SemanticAssemblyLease owns compilation only. Publication must check the
// same token again in its transaction; a successful read is not a commit fence.
type SemanticAssemblyLease struct {
	ExpansionID uuid.UUID
	Token       uuid.UUID
	Until       time.Time
	TenantID    uuid.UUID
	RunID       uuid.UUID
	WorkflowID  uuid.UUID
}

type SQLSemanticAssemblyStore struct{ DB *pgxpool.Pool }

func validSemanticAssemblyLease(l SemanticAssemblyLease) bool {
	expected, err := WorkflowID(l.RunID)
	return err == nil && l.ExpansionID != uuid.Nil && l.Token != uuid.Nil &&
		l.TenantID != uuid.Nil && l.WorkflowID == expected && !l.Until.IsZero()
}

// Open repairs a missing barrier for an initial admitted graph, including
// workflows accepted before the adoption hook was installed.
func (s SQLSemanticAssemblyStore) Open(ctx context.Context, tenant, run uuid.UUID) (uuid.UUID, error) {
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	var id *uuid.UUID
	if err := s.DB.QueryRow(ctx, "SELECT photo.open_v27_semantic_assembly($1,$2)", tenant, run).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if id == nil {
		return uuid.Nil, nil
	}
	return *id, nil
}

func (s SQLSemanticAssemblyStore) Claim(ctx context.Context) (*SemanticAssemblyLease, error) {
	if s.DB == nil {
		return nil, ErrInvalid
	}
	var l SemanticAssemblyLease
	err := s.DB.QueryRow(ctx, `SELECT expansion_id,lease_token,lease_until,tenant_id,run_id,workflow_id
 FROM photo.claim_v27_semantic_assembly()`).Scan(&l.ExpansionID, &l.Token, &l.Until, &l.TenantID, &l.RunID, &l.WorkflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validSemanticAssemblyLease(l) {
		return nil, ErrInvalid
	}
	return &l, nil
}

func (s SQLSemanticAssemblyStore) Inputs(ctx context.Context, l SemanticAssemblyLease) ([]SemanticTransferResult, error) {
	if s.DB == nil || !validSemanticAssemblyLease(l) {
		return nil, ErrInvalid
	}
	// Check actual DB lease time, never trust a caller's local Until timestamp.
	var live bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v27_semantic_assembly_lease_live($1,$2,$3,$4)`,
		l.TenantID, l.RunID, l.ExpansionID, l.Token).Scan(&live)
	if err != nil {
		return nil, err
	}
	if !live {
		return nil, ErrExpired
	}
	return LoadSemanticAssemblyInputs(ctx, s.DB, l.TenantID, l.RunID, l.WorkflowID)
}

func (s SQLSemanticAssemblyStore) Renew(ctx context.Context, l SemanticAssemblyLease) (bool, error) {
	if s.DB == nil || !validSemanticAssemblyLease(l) {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.renew_v27_semantic_assembly($1,$2)", l.ExpansionID, l.Token).Scan(&ok)
	return ok, err
}

func (s SQLSemanticAssemblyStore) Release(ctx context.Context, l SemanticAssemblyLease) (bool, error) {
	if s.DB == nil || !validSemanticAssemblyLease(l) {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.release_v27_semantic_assembly($1,$2)", l.ExpansionID, l.Token).Scan(&ok)
	return ok, err
}

// SemanticAssemblyStage contains published immutable params and the exact
// ordered canonical producer set used by the compiler.
type SemanticAssemblyStage struct {
	Params      Ref
	InputDigest string
	Transfers   []SemanticTransferResult
	Profile     json.RawMessage
}

func (s SQLSemanticAssemblyStore) Stage(ctx context.Context, l SemanticAssemblyLease, stage SemanticAssemblyStage) (uuid.UUID, error) {
	if s.DB == nil || !validSemanticAssemblyLease(l) || !validRef(stage.Params) || !digestPattern.MatchString(stage.InputDigest) || len(stage.Transfers) < 1 || len(stage.Transfers) > 5000 {
		return uuid.Nil, ErrInvalid
	}
	profile, err := canonical.Bytes(stage.Profile)
	expectedProfile, _ := CompileSemanticAssemblyProfile(256<<20, 32<<20, 8<<20)
	if err != nil || !bytes.Equal(profile, expectedProfile) {
		return uuid.Nil, ErrInvalid
	}
	refs := make([]Ref, len(stage.Transfers))
	seen := make(map[uuid.UUID]bool, len(refs))
	for i, item := range stage.Transfers {
		expected, err := TaskID(l.WorkflowID, "semantic.transfer", i)
		if err != nil || item.Ordinal != i || item.TaskID != expected || !validRef(item.Candidate) || seen[item.Candidate.ArtifactID] {
			return uuid.Nil, ErrInvalid
		}
		seen[item.Candidate.ArtifactID] = true
		refs[i] = item.Candidate
	}
	body, err := canonical.Marshal(refs)
	if err != nil {
		return uuid.Nil, err
	}
	task, err := TaskID(l.WorkflowID, "v2.semantic.assemble", 0)
	if err != nil {
		return uuid.Nil, err
	}
	var ok bool
	err = s.DB.QueryRow(ctx, `SELECT photo.stage_v27_semantic_assembly($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, l.TenantID, l.RunID, l.ExpansionID, l.Token, task, stage.Params.ArtifactID, stage.Params.SHA256, stage.InputDigest, json.RawMessage(body), json.RawMessage(profile)).Scan(&ok)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, ErrInvalid
	}
	return task, nil
}
