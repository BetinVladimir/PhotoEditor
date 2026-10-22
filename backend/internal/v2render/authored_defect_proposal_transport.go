package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// PublishTransport publishes the real native defect input transport only after
// protected SQL has created the blocked task. SQLRepository independently
// refuses an absent/unbound task at reserve/complete; this method cannot stage,
// seal or activate it. The source-authorized Params/face/protection publication
// and dedicated public-seal branch remain required admission prerequisites.
func (s SQLAuthoredDefectProposalPreparation) PublishTransport(ctx context.Context, who auth.Identity, plan QualifiedAuthoredDefectProposalPlan, bucket string, objects artifactjson.Objects) (v2input.PreparedSemanticTask, error) {
	empty := v2input.PreparedSemanticTask{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.Scheduler == nil || objects == nil || bucket == "" || plan.source.Tenant == uuid.Nil || plan.native.Run == uuid.Nil || plan.inventoryJCS == "" || plan.paramsJCS == "" {
		return empty, v2input.ErrInvalid
	}
	if who.UserID != plan.source.Actor || who.SessionID != plan.source.Session {
		return empty, v2input.ErrInvalid
	}
	var expected v2qualification.Expected
	if err := json.Unmarshal([]byte(plan.expected), &expected); err != nil {
		return empty, err
	}
	recheck := func() error {
		inventory, found, err := ReadAuthoredSegmentInventory(ctx, s.Scheduler, plan.source.Tenant, plan.source.Parent, plan.source.Instruction.Ordinal)
		if err != nil {
			return err
		}
		if !found {
			return v2input.ErrExpired
		}
		raw, err := canonical.Marshal(inventory)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, []byte(plan.inventoryJCS)) {
			return v2input.ErrExpired
		}
		return s.Qualification.Recheck(ctx, who, plan.source.Tenant, plan.source.Project, expected, plan.qualification)
	}
	if err := recheck(); err != nil {
		return empty, err
	}
	publisher := v2input.ScopedPublisher{Tenant: plan.source.Tenant, Run: plan.native.Run, Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: s.Scheduler, Bucket: bucket}, Objects: objects}}
	prepared, err := v2input.PublishSemanticDefectTask(ctx, plan.native.Frozen, publisher)
	if err != nil {
		return empty, err
	}
	if err = recheck(); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return prepared, nil
}
