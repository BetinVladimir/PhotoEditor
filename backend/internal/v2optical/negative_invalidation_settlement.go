package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// NegativeInvalidationSettlement is a committed observation, not permission to
// publish a replacement. Replacement admission must recheck its own authority.
type NegativeInvalidationSettlement struct {
 SchemaVersion int `json:"schema_version"`
 AffectedTasks int `json:"affected_tasks"`
 UnsettledAffectedTasks int `json:"unsettled_affected_tasks"`
 ConsumerRuns int `json:"consumer_runs"`
 UnsettledConsumerRuns int `json:"unsettled_consumer_runs"`
 UnsettledConsumerTasks int `json:"unsettled_consumer_tasks"`
 Settled bool `json:"settled"`
}

func decodeNegativeInvalidationSettlement(raw []byte,plan NegativeInvalidationPlan)(NegativeInvalidationSettlement,error) {
 var out NegativeInvalidationSettlement
 if len(raw)<2||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")) { return out,v2input.ErrExpired }
 if len(raw)>4096 { return out,v2input.ErrInvalid }
 normal,err:=canonical.Bytes(raw);if err!=nil { return out,v2input.ErrInvalid }
 if json.Unmarshal(normal,&out)!=nil { return NegativeInvalidationSettlement{},v2input.ErrInvalid }
 exact,err:=canonical.Marshal(out)
 if err!=nil||!bytes.Equal(exact,normal)||out.SchemaVersion!=1||out.AffectedTasks!=len(plan.AffectedTasks)||out.ConsumerRuns!=len(plan.ConsumerRuns)||
 out.UnsettledAffectedTasks<0||out.UnsettledAffectedTasks>out.AffectedTasks||out.UnsettledConsumerRuns<0||out.UnsettledConsumerRuns>out.ConsumerRuns||
 out.UnsettledConsumerTasks<0||out.UnsettledConsumerTasks>4096||(out.ConsumerRuns==0&&out.UnsettledConsumerTasks!=0)||
 out.Settled!=(out.UnsettledAffectedTasks==0&&out.UnsettledConsumerRuns==0&&out.UnsettledConsumerTasks==0) { return NegativeInvalidationSettlement{},v2input.ErrInvalid }
 return out,nil
}

// ReconcileNegativeInvalidation revalidates the decision and complete project
// closure, requests cooperative cancellation, and returns only after commit.
// An ambiguous commit returns no successful settlement snapshot; retry the same
// decision. No leases/resources are released by this coordinator.
func ReconcileNegativeInvalidation(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,review,originTask,originRun uuid.UUID,decisionHash,route string,maxTasks,maxEdges int)(NegativeInvalidationPlan,NegativeInvalidationSettlement,error) {
 var settlement NegativeInvalidationSettlement
 plan,err:=executeNegativeInvalidation(ctx,db,who,tenant,project,review,originTask,originRun,decisionHash,route,maxTasks,maxEdges,true,&settlement,false)
 if err!=nil { return NegativeInvalidationPlan{},NegativeInvalidationSettlement{},err }
 return plan,settlement,nil
}
