package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func decodeNativeTransferQualificationCandidate(raw []byte, expected v2qualification.Expected) (v2input.SemanticQualificationPin, error) {
	var zero, pin v2input.SemanticQualificationPin
	if len(raw) < 2 || len(raw) > 16384 || json.Unmarshal(raw, &pin) != nil {
		return zero, v2input.ErrInvalid
	}
	actual, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		return zero, err
	}
	wanted, err := canonical.Hash(pin)
	if err != nil || actual != wanted {
		return zero, v2input.ErrInvalid
	}
	if expected.Operation != "semantic.transfer" || expected.Version != "1" || (expected.Backend != "cuda" && expected.Backend != "mps") {
		return zero, v2input.ErrInvalid
	}
	if err := verifyQualificationConfiguration(pin, expected); err != nil {
		return zero, err
	}
	for _, digest := range []string{pin.Report.SHA256, pin.Calibration.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	return pin, nil
}

func (s SQLTransferSourceReader) discoverNativeTransferQualificationPin(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected) (v2input.SemanticQualificationPin, error) {
	var zero, pin v2input.SemanticQualificationPin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if expected.Operation != "semantic.transfer" || expected.Version != "1" || (expected.Backend != "cuda" && expected.Backend != "mps") {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(ownQualificationExpected(expected))
	if err != nil {
		return zero, err
	}
	if len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT photo.semantic_proposal_transfer_qualification_candidates($1,$2,$3,$4,$5::jsonb)", tenantID, s.ProjectID, who.UserID, who.SessionID, string(body))
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count != 0 {
				return v2qualification.ErrUnqualified
			}
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			pin, err = decodeNativeTransferQualificationCandidate(raw, expected)
			if err != nil {
				return err
			}
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 1 {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return pin, nil
}

type nativeTransferQualificationSelection struct {
	pin      v2input.SemanticQualificationPin
	expected v2qualification.Expected
	metrics  v2qualification.Metrics
}

// Reports and calibration are physically verified after unique catalog discovery.
// This selects compiler inputs only; no graph or execution authority is created.
func (s SQLTransferSourceReader) selectNativeTransferQualificationPolicy(ctx context.Context, who auth.Identity, tenantID uuid.UUID, budget int64) (nativeTransferQualificationSelection, error) {
	var zero nativeTransferQualificationSelection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if budget <= 0 || budget > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	snapshot, err := s.readQualificationPolicySnapshot(ctx, who, tenantID)
	if err != nil {
		return zero, err
	}
	policies, err := selectNativeQualificationPolicies(snapshot, []string{"semantic.transfer"})
	if err != nil {
		return zero, err
	}
	expected := policies["semantic.transfer"]
	pin, err := s.discoverNativeTransferQualificationPin(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	objects, err := s.qualificationObjects(ctx, who, tenantID, pin)
	if err != nil {
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, budget, len(snapshot.body)); err != nil {
		return zero, err
	}
	for _, object := range objects {
		limit := int64(8 << 20)
		if object.contract == "Calibration" {
			limit = 1 << 20
		}
		if object.object.Bytes < 2 || object.object.Bytes > limit {
			return zero, v2input.ErrInvalid
		}
		if err := chargeMetadata(&used, budget, int(object.object.Bytes)); err != nil {
			return zero, err
		}
	}
	proof, err := s.readTransferQualificationPreparation(ctx, who, tenantID, pin, expected)
	if err != nil {
		return zero, err
	}
	if objects[0] != proof.objects[0] || objects[1] != proof.objects[1] {
		return zero, v2input.ErrConflict
	}
	after, err := s.discoverNativeTransferQualificationPin(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	if after != pin {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, tenantID, snapshot); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeTransferQualificationSelection{pin: pin, expected: ownQualificationExpected(expected), metrics: proof.metrics}, nil
}

// Only selections produced by physical report/calibration verification reach
// this compiler-input assembler. It creates no execution/admission authority.
func applyNativeTransferQualificationSelection(base TransferPreparation, selected nativeTransferQualificationSelection) (TransferPreparation, error) {
	var zero TransferPreparation
	owned := ownTransferPreparation(base)
	if err := validateTransferCapacityConfiguration(owned); err != nil {
		return zero, err
	}
	if owned.Qualification != (v2input.SemanticQualificationPin{}) && owned.Qualification != selected.pin {
		return zero, v2input.ErrConflict
	}
	if !reflect.DeepEqual(owned.Expected, v2qualification.Expected{}) && !reflect.DeepEqual(owned.Expected, selected.expected) {
		return zero, v2input.ErrConflict
	}
	costs := owned.Costs
	if costs == (v2input.SemanticQualifiedCosts{}) {
		var err error
		costs, err = transferCostsFromVerifiedMetrics(selected.metrics)
		if err != nil {
			return zero, err
		}
	}
	if err := validateTransferMeasuredCosts(costs, selected.metrics); err != nil {
		return zero, err
	}
	if costs.RAMBytes > owned.RAM || costs.VRAMBytes > owned.VRAM || costs.ScratchBytes > owned.Scratch {
		return zero, v2input.ErrInvalid
	}
	owned.Qualification, owned.Expected, owned.Costs = selected.pin, ownQualificationExpected(selected.expected), costs
	if err := validateTransferPolicyConfiguration(owned); err != nil {
		return zero, err
	}
	return owned, nil
}

func (s SQLTransferSourceReader) prepareNativeTransferRuntimeConfiguration(ctx context.Context, who auth.Identity, tenantID uuid.UUID, base TransferPreparation) (TransferPreparation, error) {
	var zero TransferPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	base = ownTransferPreparation(base)
	if err := validateTransferCapacityConfiguration(base); err != nil {
		return zero, err
	}
	selected, err := s.selectNativeTransferQualificationPolicy(ctx, who, tenantID, base.MetadataBytes)
	if err != nil {
		return zero, err
	}
	return applyNativeTransferQualificationSelection(base, selected)
}
