import type {OwnerAPI} from './owner-api';
import type {PerspectiveAttemptRecord} from './perspective-attempt';
import type {PerspectiveAttemptStore} from './perspective-attempt-store';
import {freezePerspectiveIntent,type PerspectiveIntent} from './perspective-intent';
/** Only an acknowledged attempt may yield its journal slot. Validate current
 * workflow authority and source before discarding the old local record. */
export async function prepareNextPerspective(api:Pick<OwnerAPI,'get'|'v2Binding'|'createRenderInputSnapshot'>,asset:string,record:PerspectiveAttemptRecord,store:Pick<PerspectiveAttemptStore,'clear'>,signal:AbortSignal):Promise<PerspectiveIntent>{
 const owned=structuredClone(record);
 const active=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_NEXT_CANCELLED');};
 active();if(!owned.ticket)throw new Error('PERSPECTIVE_ATTEMPT_UNRESOLVED');
 const status=await api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.ticket.workflow_id)}`,signal);
 active();if(status.run_id!==owned.ticket.run_id||status.workflow_id!==owned.ticket.workflow_id)throw new Error('PERSPECTIVE_TICKET_SCOPE_CHANGED');
 const base=await api.v2Binding(asset,signal);
 active();if(base.asset_version_id!==owned.body.params.base.asset_version_id)throw new Error('PERSPECTIVE_SOURCE_CHANGED');
 const snapshot=await api.createRenderInputSnapshot(owned.project,{sources:[base]},crypto.randomUUID(),signal);
 active();const next=freezePerspectiveIntent({...owned.body,snapshot_id:snapshot.id,params:{...owned.body.params,base,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null}});
 await store.clear(owned.key);active();return next;
}
