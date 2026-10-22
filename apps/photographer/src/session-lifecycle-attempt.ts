import type {ShootSession,ShootSessionUpdate} from './capture-types';
import {parseSessionPolicyAttempt,type SessionPolicyDraftStore} from './session-policy-draft';

/** Lifecycle and policy edits share one mutation journal and recovery UI. */
export async function persistSessionLifecycleAttempt(store:Pick<SessionPolicyDraftStore,'read'|'reserveLifecycle'>,session:ShootSession,request:ShootSessionUpdate,key:string):Promise<void>{
 const attempt=parseSessionPolicyAttempt({request,key},session.id);
 if(!attempt)throw new Error('SESSION_LIFECYCLE_ATTEMPT_INVALID');
 await store.reserveLifecycle({schema_version:1,session_id:session.id,base_revision:session.revision,name:session.name,policy:structuredClone(session.policy),uncertain:true,attempt,saved_at:Date.now()});
 const saved=await store.read();
 if(saved?.attempt?.key!==key||JSON.stringify(saved.attempt.request)!==JSON.stringify(attempt.request))throw new Error('SESSION_LIFECYCLE_ATTEMPT_NOT_PERSISTED');
}
