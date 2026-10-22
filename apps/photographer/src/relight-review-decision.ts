import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {Review} from './catalog-types';
import {ownRenderReviewBinding} from './review-binding';

type Decision='accept'|'reject'|'skip';
export type RelightReviewDecisionIntent={schema_version:1;tenant:string;actor:string;review:Review;decision:Decision;decision_hash:string};
type API={review:(id:string,signal?:AbortSignal)=>Promise<Review>;decide:(review:Review,decision:Decision,accepted:string[],rejected:string[],key?:string,signal?:AbortSignal)=>Promise<Review>};
const uuid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';

export function relightReviewDecisionIntent(tenant:string,actor:string,review:Review,decision:Decision):RelightReviewDecisionIntent{
 const owned=ownRenderReviewBinding(structuredClone(review));
 if(!uuid(tenant)||!uuid(actor)||!uuid(owned.id)||!uuid(owned.run_id)||owned.kind!=='image.relight'||owned.state!=='pending'||owned.revision!==1||!['accept','reject','skip'].includes(decision))throw new Error('RELIGHT_DECISION_INVALID');
 const payload={version:'candidate_decision_v1',tenant_id:tenant,review_id:owned.id,run_id:owned.run_id,actor_id:actor,kind:owned.kind,decision,
  selection:{version:'candidate_review_v1',candidate:owned.candidate,base:owned.base,revision:owned.revision+1}};
 return {schema_version:1,tenant,actor,review:owned,decision,decision_hash:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(payload))))};
}

export function readRelightReviewDecisionIntent(value:RelightReviewDecisionIntent):RelightReviewDecisionIntent{
 if(!value||Object.keys(value).sort().join(',')!=='actor,decision,decision_hash,review,schema_version,tenant'||value.schema_version!==1)throw new Error('RELIGHT_DECISION_INVALID');
 const owned=relightReviewDecisionIntent(value.tenant,value.actor,value.review,value.decision);
 if(owned.decision_hash!==value.decision_hash)throw new Error('RELIGHT_DECISION_INVALID');
 return owned;
}

function binding(review:Review){return canonicalJSON({id:review.id,run_id:review.run_id,kind:review.kind,candidate_hash:review.candidate_hash,candidate:review.candidate,base:review.base})}
export function relightReviewDecisionReceipt(value:Review,intent:RelightReviewDecisionIntent):Review|null{
 const current=ownRenderReviewBinding(structuredClone(value));
 if(binding(current)!==binding(intent.review))throw new Error('RELIGHT_DECISION_SCOPE_CHANGED');
 if(current.state==='pending'&&current.revision===intent.review.revision)return null;
 const route=intent.decision==='accept'?'candidate':intent.decision==='reject'?'base':'skip';
 if(current.state!=='decided'||current.revision!==intent.review.revision+1||current.decision!==intent.decision||current.decision_route!==route||current.decision_hash!==intent.decision_hash)throw new Error('RELIGHT_DECISION_CONFLICT');
 return current;
}

// A matching actor-bound decision receipt resolves an ambiguous response.
// Never issue a second decision within one call after a failed mutation.
export async function recoverRelightReviewDecision(api:API,value:RelightReviewDecisionIntent,key:string,signal?:AbortSignal):Promise<Review>{
 const intent=readRelightReviewDecisionIntent(structuredClone(value));
 if(!uuid(key))throw new Error('RELIGHT_DECISION_KEY_INVALID');
 const active=()=>{signal?.throwIfAborted()};active();
 const previous=relightReviewDecisionReceipt(await api.review(intent.review.id,signal),intent);active();
 if(previous)return previous;
 try{
  await api.decide(structuredClone(intent.review),intent.decision,[],[],key,signal);active();
  // Decision response is a summary; reload physical source-bound detail.
  const accepted=relightReviewDecisionReceipt(await api.review(intent.review.id,signal),intent);active();
  if(!accepted)throw new Error('RELIGHT_DECISION_RECEIPT_INVALID');
  return accepted;
 }catch(error){
  active();
  const recovered=relightReviewDecisionReceipt(await api.review(intent.review.id,signal),intent);active();
  if(recovered)return recovered;
  throw error;
 }
}
