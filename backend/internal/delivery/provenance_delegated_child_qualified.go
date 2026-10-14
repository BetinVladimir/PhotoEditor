package delivery

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// CompileQualifiedCurrentDelegatedSignerChild derives the final execution
// profile from the installed workload measurement. The first compile uses
// protocol floors only to freeze the intent and prove live policy/identity;
// that provisional task is never returned or admitted. A second current read
// and compile must agree with the same source and measured profile.
func CompileQualifiedCurrentDelegatedSignerChild(ctx context.Context,
	db *pgxpool.Pool, lease ExpansionLease, slot Slot, bucket string,
	lineageRef v2input.Ref, lineageBody []byte,
	registry *provenance.SigningPolicyRegistry,
	qualifier DelegatedSigningResources, now time.Time,
) (SignerDelegationScope, provenance.ReadySigningExport,
	provenance.FrozenSigningIntent, provenance.FrozenSigningTask, error) {
	var scope SignerDelegationScope
	var ready provenance.ReadySigningExport
	var intent provenance.FrozenSigningIntent
	var task provenance.FrozenSigningTask
	if err := ctx.Err(); err != nil { return scope, ready, intent, task, err }
	if qualifier == nil { return scope, ready, intent, task, v2input.ErrInvalid }
	provisional := provenance.SigningTaskResources{
		ContractsSHA: strings.Repeat("0", 64), RAMBytes: 128 << 20,
		ScratchBytes: 1 << 30, OutputBytes: (512 << 20) + (16 << 20),
		MaxInputJSONBytes: 128 << 20,
	}
	firstScope, firstReady, firstIntent, _, err :=
		CompileCurrentDelegatedSignerChild(ctx, db, lease, slot, bucket,
			lineageRef, lineageBody, registry, provisional, now)
	if err != nil { return scope, ready, intent, task, err }
	measured, err := qualifier.QualifyDelegatedSigningResources(ctx,
		firstScope.ActorID, firstIntent, firstReady)
	if err != nil { return scope, ready, intent, task, err }
	currentScope, currentReady, currentIntent, currentTask, err :=
		CompileCurrentDelegatedSignerChild(ctx, db, lease, slot, bucket,
			lineageRef, lineageBody, registry, measured, now)
	if err != nil { return scope, ready, intent, task, err }
	if currentScope != firstScope || currentReady != firstReady ||
		currentIntent.WorkflowID() != firstIntent.WorkflowID() ||
		currentIntent.RunID() != firstIntent.RunID() ||
		currentIntent.TaskID() != firstIntent.TaskID() ||
		currentIntent.RequestSHA() != firstIntent.RequestSHA() ||
		string(currentIntent.ParamsBytes()) != string(firstIntent.ParamsBytes()) ||
		string(currentIntent.LineageBytes()) != string(firstIntent.LineageBytes()) ||
		currentTask.Resources() != measured {
		return scope, ready, intent, task, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil { return scope, ready, intent, task, err }
	return currentScope, currentReady, currentIntent, currentTask, nil
}
