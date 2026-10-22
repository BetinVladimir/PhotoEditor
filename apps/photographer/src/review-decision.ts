import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {Review} from './catalog-types';
type Decision='accept'|'reject'|'skip';
export type ReviewDecisionIntent={schema_version:1;tenant:string;actor:string;review:Review;decision:Decision;accepted:string[];rejected:string[];decision_hash:string};
type API={review:(id:string,signal?:AbortSignal)=>Promise<Review>;decide:(review:Review,decision:Decision,accepted:string[],rejected:string[],key?:string,signal?:AbortSignal)=>Promise<Review>};
const uuid=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const digest=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function ref(value:Review['base']){if(!value||Object.keys(value).sort().join(',')!=='artifact_id,sha256'||!uuid(value.artifact_id)||!digest(value.sha256))throw new Error('REVIEW_BINDING_INVALID');return {...value}}
function binding(r:Review){const common={id:r.id,run_id:r.run_id,kind:r.kind,candidate_hash:r.candidate_hash};return canonicalJSON(r.kind==='defects'?{...common,proposal:ref(r.proposal),image:ref(r.image),label_map:ref(r.label_map),candidate_ids:[...(r.candidate_ids??[])].sort()}:r.kind==='sensor_dust'?{...common,proposal:ref(r.proposal),base:ref(r.base),candidate_ids:[...(r.candidate_ids??[])].sort()}:{...common,candidate:ref(r.candidate),base:ref(r.base)})}
export function reviewDecisionIntent(tenant:string,actor:string,review:Review,decision:Decision,accepted:string[]=[],rejected:string[]=[]):ReviewDecisionIntent{
 const r=structuredClone(review),selectionKind=r.kind==='defects'||r.kind==='sensor_dust';
 if(![tenant,actor,r.id,r.run_id].every(uuid)||r.state!=='pending'||!Number.isSafeInteger(r.revision)||r.revision<1||!Number.isSafeInteger(r.revision+1)||!digest(r.candidate_hash)||!r.kind||r.kind==='image.relight'||!['accept','reject','skip'].includes(decision))throw new Error('REVIEW_DECISION_INVALID');
 binding(r);
 const ids=r.candidate_ids??[];if(ids.length>4096||ids.some(id=>!digest(id))||new Set(ids).size!==ids.length)throw new Error('REVIEW_CANDIDATES_INVALID');
 const a=[...accepted].sort(),b=[...rejected].sort(),all=[...a,...b];
 if(all.length>4096||new Set(all).size!==all.length||all.some(id=>!ids.includes(id))||(!selectionKind||decision!=='accept')&&all.length>0)throw new Error('REVIEW_SELECTION_INVALID');
 if((selectionKind?r.proposal:r.candidate)?.sha256!==r.candidate_hash)throw new Error('REVIEW_BINDING_INVALID');
 const revision=r.revision+1;
 const selection=r.kind==='defects'?{version:'defect_review_v1',proposal:r.proposal,image:r.image,label_map:r.label_map,revision,accepted_candidate_ids:a,rejected_candidate_ids:b}:r.kind==='sensor_dust'?{version:'sensor_dust_review_v1',proposal:r.proposal,base:r.base,revision,accepted_candidate_ids:a,rejected_candidate_ids:b}:{version:'candidate_review_v1',candidate:r.candidate,base:r.base,revision};
 const payload={version:r.kind==='defects'?'defect_decision_v1':r.kind==='sensor_dust'?'sensor_dust_decision_v1':'candidate_decision_v1',tenant_id:tenant,review_id:r.id,run_id:r.run_id,actor_id:actor,...(!selectionKind?{kind:r.kind}:{}),decision,selection};
 return {schema_version:1,tenant,actor,review:r,decision,accepted:a,rejected:b,decision_hash:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(payload))))};
}
export function readReviewDecisionIntent(value:ReviewDecisionIntent){
 if(!value||value.schema_version!==1||Object.keys(value).sort().join(',')!=='accepted,actor,decision,decision_hash,rejected,review,schema_version,tenant'||!Array.isArray(value.accepted)||!Array.isArray(value.rejected))throw new Error('REVIEW_DECISION_INVALID');
 const owned=reviewDecisionIntent(value.tenant,value.actor,value.review,value.decision,value.accepted,value.rejected);if(owned.decision_hash!==value.decision_hash)throw new Error('REVIEW_DECISION_INVALID');return owned;
}
export function reviewDecisionReceipt(value:Review,intent:ReviewDecisionIntent):Review|null{
 const current=structuredClone(value);if(binding(current)!==binding(intent.review))throw new Error('REVIEW_DECISION_SCOPE_CHANGED');
 if(current.state==='pending'&&current.revision===intent.review.revision)return null;
 const selective=intent.review.kind==='defects'||intent.review.kind==='sensor_dust';
 const route=intent.decision==='skip'?'skip':selective?(intent.decision==='accept'&&intent.accepted.length?'repair':'base'):intent.decision==='accept'?'candidate':'base';
 if(current.state!=='decided'||current.revision!==intent.review.revision+1||current.decision!==intent.decision||current.decision_route!==route||current.decision_hash!==intent.decision_hash)throw new Error('REVIEW_DECISION_CONFLICT');return current;
}
export async function recoverReviewDecision(api:API,value:ReviewDecisionIntent,key:string,signal?:AbortSignal){
 const intent=readReviewDecisionIntent(value);if(!uuid(key))throw new Error('REVIEW_DECISION_KEY_INVALID');const active=()=>signal?.throwIfAborted();active();
 const prior=reviewDecisionReceipt(await api.review(intent.review.id,signal),intent);active();if(prior)return prior;
 try{await api.decide(structuredClone(intent.review),intent.decision,[...intent.accepted],[...intent.rejected],key,signal);active();const receipt=reviewDecisionReceipt(await api.review(intent.review.id,signal),intent);active();if(!receipt)throw new Error('REVIEW_DECISION_RECEIPT_INVALID');return receipt}
 catch(error){active();const receipt=reviewDecisionReceipt(await api.review(intent.review.id,signal),intent);active();if(receipt)return receipt;throw error}
}
