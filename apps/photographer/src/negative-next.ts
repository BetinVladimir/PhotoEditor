import type {OwnerAPI} from './owner-api';
import type {NegativeAttemptRecord} from './negative-attempt';
import type {NegativeAttemptStore} from './negative-attempt-store';
import {freezeNegativeIntent,type NegativeIntent} from './negative-intent';
/** New work is explicit and permitted only after a durable acknowledgement.
 * Do not clear recovery data until current authority and new input are ready. */
export async function prepareNextNegative(api:Pick<OwnerAPI,'get'|'v2Binding'|'negativeCapabilities'|'createRenderInputSnapshot'>,asset:string,record:NegativeAttemptRecord,store:Pick<NegativeAttemptStore,'clear'>,signal:AbortSignal):Promise<NegativeIntent> {
 const owned=structuredClone(record),active=()=>{if(signal.aborted)throw new Error('NEGATIVE_NEXT_CANCELLED')};
 active();if(!owned.ticket)throw new Error('NEGATIVE_ATTEMPT_UNRESOLVED');
 const status=await api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.ticket.workflow_id)}`,signal);
 active();if(status.run_id!==owned.ticket.run_id||status.workflow_id!==owned.ticket.workflow_id)throw new Error('NEGATIVE_TICKET_SCOPE_CHANGED');
 const base=await api.v2Binding(asset,signal);active();
 const old=owned.body.params.base;
 if(base.asset_version_id!==old.asset_version_id||base.source.artifact_id!==old.source.artifact_id||base.source.sha256!==old.source.sha256||base.canvas_hash!==old.canvas_hash)throw new Error('NEGATIVE_SOURCE_CHANGED');
 const configured=await api.negativeCapabilities(owned.project,signal);active();
 const snapshot=await api.createRenderInputSnapshot(owned.project,{sources:[base]},crypto.randomUUID(),signal);active();
 const next=freezeNegativeIntent({...owned.body,snapshot_id:snapshot.id,params:{...owned.body.params,base,calibration:configured.calibration}});
 await store.clear(owned.key);active();return next;
}
