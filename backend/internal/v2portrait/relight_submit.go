package v2portrait

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// SubmitRelight executes the complete server-owned submission on an existing
// authorized draft. Each stage owns its transaction; object IO happens outside
// transactions. Resuming a committed run uses ReadRelightQueuedReceipt, not a
// blind resubmission. No draft/run is created by this method.
func (s RelightBootstrapCoordinator) SubmitRelight(ctx context.Context, objects artifactjson.Objects, request RelightBootstrapRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if objects == nil {
		return nil, v2input.ErrInvalid
	}
	prepared, err := s.Prepare(ctx, request)
	if err != nil {
		return nil, err
	}
	frozen, err := s.PublishPreparedRelightTask(ctx, objects, prepared)
	if err != nil {
		return nil, err
	}
	graph, err := s.PublishRelightTransport(ctx, objects, prepared, frozen)
	if err != nil {
		return nil, err
	}
	if err = s.RegisterRelightTransport(ctx, prepared, frozen, graph); err != nil {
		return nil, err
	}
	admissionErr := s.AdmitRelightGraph(ctx, prepared, frozen)
	// A real commit may have succeeded even if the acknowledgement was lost.
	// Recovery rechecks current authority and must bind the original descriptor.
	receipt, err := s.ReadRelightQueuedReceipt(ctx, request.Who, request.Tenant, request.Run)
	if err != nil {
		if admissionErr != nil {
			return nil, admissionErr
		}
		return nil, err
	}
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return nil, err
	}
	digest, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		return nil, err
	}
	var wire struct {
		DescriptorSHA string `json:"descriptor_sha"`
		Task          string `json:"task_id"`
		Workflow      string `json:"workflow_id"`
	}
	if json.Unmarshal(receipt, &wire) != nil || wire.DescriptorSHA != digest || wire.Task != prepared.Intent.OwnerID.String() || wire.Workflow != request.Workflow.String() {
		return nil, v2input.ErrInvalid
	}
	return receipt, nil
}
