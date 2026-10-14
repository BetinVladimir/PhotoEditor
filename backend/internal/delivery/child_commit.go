package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ChildConfig is the immutable run configuration expected by cursor admission.
// Existing export readiness is deliberately not claimed by this descriptor.
func (p PreparePlan) ChildConfig(parent uuid.UUID, child ExportChildIntent, exportID uuid.UUID) ([]byte, string, error) {
	if exportID == uuid.Nil {
		return nil, "", v2input.ErrInvalid
	}
	request, _, err := p.ExportRequest(parent, child)
	if err != nil {
		return nil, "", err
	}
	policy, policyErr := p.MetadataPolicy()
	if policyErr != nil || policy.MetadataPolicy != "strip" { return nil, "", v2input.ErrInvalid }
	var proof PrepareProof
	if json.Unmarshal(p.proofJCS, &proof) != nil || proof.ProjectID == uuid.Nil {
		return nil, "", v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(struct {
		QueueClass string `json:"queue_class"`
		ProjectID uuid.UUID           `json:"project_id"`
		Request   FrozenExportRequest `json:"v30_delivery_export"`
		ExportID  uuid.UUID           `json:"export_id"`
	}{"batch", proof.ProjectID, request, exportID})
	if err != nil {
		return nil, "", err
	}
	return raw, digest(raw), nil
}

// CommitChild binds an already sealed child/export to its pending slot and
// advances one cursor step atomically. It cannot create or dispatch child runs.
// Caller may update its local cursor only after success. SQL validates exact
// replay, current lease/source authority and the saved child configuration.
func (s ExpansionLeaseStore) CommitChild(ctx context.Context, lease ExpansionLease, p PreparePlan, child ExportChildIntent, exportID uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil {
		return domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	if !lease.valid() || lease.Workflow != p.workflow || lease.ParentTask != p.task || lease.SlotCount != p.preparation.SlotCount || exportID == uuid.Nil || child.SlotIndex > lease.Cursor {
		return v2input.ErrInvalid
	}
	_, raw, err := p.ExportRequest(lease.Run, child)
	if err != nil {
		return err
	}
	var accepted bool
	err = s.DB.QueryRow(ctx, `SELECT photo.commit_v30_delivery_child($1,$2,$3,$4,$5)`, lease.Expansion, lease.Token, child.SlotIndex, string(raw), exportID).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_CHILD_STALE", 409)
	}
	return nil
}
