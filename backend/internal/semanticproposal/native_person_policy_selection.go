package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Selects the one admission-enabled DBA person policy and imported report.
// Byte verification is independent of discovery. Producer evidence, coverage
// and transactional admission authority remain required for every photograph.
func (s SQLTransferSourceReader) selectNativePersonProtectionPolicy(ctx context.Context, who auth.Identity, tenantID uuid.UUID, budget int64) (TransferPersonProtection, error) {
	var zero TransferPersonProtection
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
	selected, err := selectNativeQualificationPolicies(snapshot, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	expected := selected["mask.person"]
	report, err := s.discoverPersonQualificationReport(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	pin, err := s.personQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	var used int64
	if pin.object.Bytes < 2 || pin.object.Bytes > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	if err := chargeMetadata(&used, budget, len(snapshot.body), int(pin.object.Bytes)); err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, report)
	if err != nil {
		return zero, err
	}
	if _, err := v2qualification.VerifyTyped(raw, report.SHA256, expected); err != nil {
		return zero, err
	}
	current, err := s.personQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if current != pin {
		return zero, v2input.ErrConflict
	}
	after, err := s.discoverPersonQualificationReport(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	if after != report {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, tenantID, snapshot); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return TransferPersonProtection{Report: report, Expected: ownQualificationExpected(expected)}, nil
}
