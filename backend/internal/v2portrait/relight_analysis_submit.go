package v2portrait

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type analysisSubmissionSteps interface {
	ReadQueuedReceipt(context.Context, auth.Identity, RelightAnalysisPlanRequest) (RelightAnalysisReceipt, error)
	PrepareParams(context.Context, auth.Identity, RelightAnalysisPlanRequest) (PreparedRelightAnalysis, error)
	PublishTransport(context.Context, auth.Identity, RelightAnalysisPlanRequest, PreparedRelightAnalysis) (v2input.GraphTask, error)
	Admit(context.Context, auth.Identity, RelightAnalysisPlanRequest, PreparedRelightAnalysis) error
}

// Submit recovers a historical acknowledgement before any publication. Unknown
// commits never cause an automatic new run/key; all retries use the same frozen
// request. Receipt recovery cannot substitute for current worker authority.
func (c RelightAnalysisCoordinator) Submit(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest) (RelightAnalysisReceipt, error) {
	return submitRelightAnalysis(ctx, c, who, request)
}
func submitRelightAnalysis(ctx context.Context, steps analysisSubmissionSteps, who auth.Identity, request RelightAnalysisPlanRequest) (RelightAnalysisReceipt, error) {
	var zero RelightAnalysisReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if steps == nil || who.UserID != request.Actor || who.SessionID != request.Session {
		return zero, v2input.ErrInvalid
	}
	// Own source bytes across all phases and validate the entire frozen request.
	request.SourceRaw = append([]byte(nil), request.SourceRaw...)
	plan, err := CompileRelightAnalysisPlan(ctx, request)
	if err != nil {
		return zero, err
	}
	read := func() (RelightAnalysisReceipt, error) {
		receipt, err := steps.ReadQueuedReceipt(ctx, who, request)
		if err != nil {
			return zero, err
		}
		raw, err := canonical.Marshal(receipt)
		if err != nil {
			return zero, err
		}
		return parseAnalysisReceipt(ctx, plan, request, raw)
	}
	receipt, err := read()
	if err == nil {
		return receipt, nil
	}
	if !errors.Is(err, v2input.ErrExpired) {
		return zero, err
	}
	if IsRelightReplayOnly(ctx) { return zero, err }
	recoverFailure := func(original error) (RelightAnalysisReceipt, error) {
		if ctx.Err() != nil {
			return zero, original
		}
		receipt, recoveryErr := read()
		if recoveryErr == nil {
			return receipt, nil
		}
		// Preserve the original phase error. Caller may retry the exact request;
		// no second publication/activation attempt occurs in this invocation.
		return zero, errors.Join(original, recoveryErr)
	}
	prepared, err := steps.PrepareParams(ctx, who, request)
	if err != nil {
		return recoverFailure(err)
	}
	descriptor, err := prepared.plan.Descriptor(ctx)
	if err != nil {
		return recoverFailure(err)
	}
	expected, err := plan.Descriptor(ctx)
	if err != nil || string(expected) != string(descriptor) || string(plan.snapshot.Config) != string(prepared.plan.snapshot.Config) || prepared.params.ID != plan.snapshot.ParamsDocument || prepared.params.SHA256 != plan.snapshot.ParamsSHA {
		return recoverFailure(v2input.ErrInvalid)
	}
	graph, err := steps.PublishTransport(ctx, who, request, prepared)
	if err != nil {
		return recoverFailure(err)
	}
	if graph.ID != plan.snapshot.Task {
		return recoverFailure(v2input.ErrInvalid)
	}
	if _, err = plan.BindInputClosure(ctx, graph.Built); err != nil {
		return recoverFailure(err)
	}
	if err = steps.Admit(ctx, who, request, prepared); err != nil {
		return recoverFailure(err)
	}
	// Never synthesize success from a nil admission error: read committed receipt.
	return read()
}
