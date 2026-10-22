import {canonicalJSON,APIError} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {ArtifactRef,Review} from './catalog-types';
export type CullingItem={item_id:string;asset_id:string;asset_version_id:string;scene_id:string;exact_group_id:string;burst_group_id:string;rank:number;score:number;recommended:boolean;manual:boolean;eye_review:boolean;eye_state:'open'|'closed'|'unknown'};
export type CullingPage={review_id:string;run_id:string;revision:number;state:'pending'|'decided';candidate_hash:string;snapshot_id:string;snapshot_digest:string;never_delete:true;total:number;items:CullingItem[];next_cursor:string|null;groups:ArtifactRef;proposal:ArtifactRef;selected_item_ids:string[]|null;confirmed_eyes_item_ids:string[]|null};
export type CullingSelection=Omit<CullingPage,'next_cursor'>;
export type CullingDecisionRequest={expected_revision:number;candidate_hash:string;selected_item_ids:string[];confirmed_eyes_item_ids:string[];never_delete:true};
export type CullingDecisionIntent={schema_version:1;tenant:string;actor:string;selection:CullingSelection;request:CullingDecisionRequest;decision_hash:string};
const fail=():never=>{throw new Error('CULLING_SELECTION_INVALID')};
export const cullingUUID=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===[...keys].sort().join(',');}
function ref(v:unknown):ArtifactRef{if(!fields(v,['artifact_id','sha256'])||!cullingUUID(v.artifact_id)||!sha(v.sha256))return fail();return {artifact_id:v.artifact_id,sha256:v.sha256 as string};}
function ids(v:unknown,minimum=0):string[]{if(!Array.isArray(v)||v.length<minimum||v.length>5000||v.some(id=>!cullingUUID(id))||new Set(v).size!==v.length)return fail();return [...v].sort();}
const pageKeys=['review_id','run_id','revision','state','candidate_hash','snapshot_id','snapshot_digest','never_delete','total','items','next_cursor','groups','proposal','selected_item_ids','confirmed_eyes_item_ids'];
export function readCullingPage(value:unknown,review:Pick<Review,'id'|'run_id'|'revision'|'state'|'kind'|'candidate_hash'>):CullingPage{
 if(!fields(value,pageKeys)||review.kind!=='culling'||value.review_id!==review.id||value.run_id!==review.run_id||value.revision!==review.revision||value.state!==review.state||value.candidate_hash!==review.candidate_hash||![value.review_id,value.run_id,value.snapshot_id].every(cullingUUID)||!sha(value.candidate_hash)||!sha(value.snapshot_digest)||!Number.isSafeInteger(value.revision)||(value.revision as number)<1||!['pending','decided'].includes(value.state as string)||value.never_delete!==true||!Number.isInteger(value.total)||(value.total as number)<1||(value.total as number)>5000||!Array.isArray(value.items)||value.items.length>100||value.items.length<1)return fail();
 const proposal=ref(value.proposal),groups=ref(value.groups);if(proposal.sha256!==value.candidate_hash)return fail();
 const observed=new Set<string>();const items=value.items.map(raw=>{
  if(!fields(raw,['item_id','asset_id','asset_version_id','scene_id','exact_group_id','burst_group_id','rank','score','recommended','manual','eye_review','eye_state'])||![raw.item_id,raw.asset_id,raw.asset_version_id].every(cullingUUID)||observed.has(raw.item_id as string)||!['scene_id','exact_group_id','burst_group_id'].every(k=>typeof raw[k]==='string'&&(raw[k] as string).length>0&&(raw[k] as string).length<=80)||!Number.isSafeInteger(raw.rank)||(raw.rank as number)<1||(raw.rank as number)>5000||typeof raw.score!=='number'||!Number.isFinite(raw.score)||raw.score<0||raw.score>1||!['recommended','manual','eye_review'].every(k=>typeof raw[k]==='boolean')||!['open','closed','unknown'].includes(raw.eye_state as string))return fail();
  observed.add(raw.item_id as string);return structuredClone(raw) as unknown as CullingItem;
 });
 if(value.next_cursor!==null&&(typeof value.next_cursor!=='string'||!value.next_cursor||value.next_cursor.length>2048||items.length!==100))return fail();
 let selected:string[]|null=null,confirmed:string[]|null=null;
 if(value.state==='pending'){if(value.selected_item_ids!==null||value.confirmed_eyes_item_ids!==null)return fail();}
 else{selected=ids(value.selected_item_ids,1);confirmed=ids(value.confirmed_eyes_item_ids);if(canonicalJSON(selected)!==canonicalJSON(value.selected_item_ids)||canonicalJSON(confirmed)!==canonicalJSON(value.confirmed_eyes_item_ids)||confirmed.some(id=>!selected!.includes(id)))return fail();}
 return {...value,items,groups,proposal,selected_item_ids:selected,confirmed_eyes_item_ids:confirmed} as CullingPage;
}
function pins(page:CullingPage|CullingSelection){const {items,...root}=page as CullingPage;delete (root as Partial<CullingPage>).next_cursor;return canonicalJSON(root);}
export async function loadCullingSelection(api:{cullingItems:(review:Review,after:string|null,signal:AbortSignal)=>Promise<unknown>},review:Review,signal:AbortSignal):Promise<CullingSelection>{
 let after:string|null=null,first:CullingPage|undefined;const cursors=new Set<string>(),seen=new Set<string>(),items:CullingItem[]=[];
 for(let page=0;page<50;page++){
  signal.throwIfAborted();const current=readCullingPage(await api.cullingItems(review,after,signal),review);signal.throwIfAborted();
  if(first&&pins(first)!==pins(current))return fail();first??=current;
  for(const item of current.items){if(seen.has(item.item_id)||items.length>=current.total)return fail();seen.add(item.item_id);items.push(item);}
  if(current.next_cursor===null){if(items.length!==current.total)return fail();const {next_cursor,...root}=first;const result={...root,items};if(result.selected_item_ids?.some(id=>!seen.has(id))||result.confirmed_eyes_item_ids?.some(id=>!seen.has(id)))return fail();return result;}
  if(cursors.has(current.next_cursor)||items.length>=current.total)return fail();cursors.add(current.next_cursor);after=current.next_cursor;
 }
 return fail();
}
export function cullingDecisionIntent(tenant:string,actor:string,selection:CullingSelection,selected:string[],confirmed:string[]):CullingDecisionIntent{
 if(![tenant,actor].every(cullingUUID)||selection.state!=='pending'||!Number.isSafeInteger(selection.revision+1)||selection.never_delete!==true||selection.items.length!==selection.total||selection.total<1||selection.total>5000||new Set(selection.items.map(i=>i.item_id)).size!==selection.total)return fail();
 const chosen=ids(selected,1),eyes=ids(confirmed),items=new Map(selection.items.map(item=>[item.item_id,item]));
 if(chosen.some(id=>!items.has(id))||eyes.some(id=>!chosen.includes(id))||chosen.some(id=>items.get(id)!.eye_review&&!eyes.includes(id)))return fail();
 const request:CullingDecisionRequest={expected_revision:selection.revision,candidate_hash:selection.candidate_hash,selected_item_ids:chosen,confirmed_eyes_item_ids:eyes,never_delete:true};
 const payload={version:'culling_decision_v1',tenant_id:tenant,review_id:selection.review_id,run_id:selection.run_id,actor_id:actor,candidate_hash:selection.candidate_hash,snapshot_id:selection.snapshot_id,snapshot_digest:selection.snapshot_digest,selected_item_ids:chosen,confirmed_eyes_item_ids:eyes,never_delete:true};
 return {schema_version:1,tenant,actor,selection:structuredClone(selection),request,decision_hash:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(payload))))};
}
export function readCullingDecisionIntent(value:CullingDecisionIntent){
 if(!fields(value,['schema_version','tenant','actor','selection','request','decision_hash'])||value.schema_version!==1)return fail();
 const v=value as CullingDecisionIntent,s=v.selection;if(!s||!Array.isArray(s.items)||s.items.length<1||s.items.length>5000)return fail();
 // Reuse the wire validator for every persisted page; no local cache can turn
 // a partial or rebound projection into a valid mutation.
 const review={id:s.review_id,run_id:s.run_id,revision:s.revision,state:s.state,kind:'culling',candidate_hash:s.candidate_hash};
 for(let start=0;start<s.items.length;start+=100)readCullingPage({...s,items:s.items.slice(start,start+100),next_cursor:null},review);
 const owned=cullingDecisionIntent(v.tenant,v.actor,s,v.request.selected_item_ids,v.request.confirmed_eyes_item_ids);
 if(canonicalJSON(owned)!==canonicalJSON(v))return fail();return owned;
}
export function cullingDecisionReceipt(review:Review,intent:CullingDecisionIntent):Review|null{
 const s=intent.selection;if(review.id!==s.review_id||review.run_id!==s.run_id||review.kind!=='culling'||review.candidate_hash!==s.candidate_hash)throw new Error('CULLING_REVIEW_CHANGED');
 if(review.state==='pending'&&review.revision===s.revision)return null;
 if(review.state!=='decided'||review.revision!==s.revision+1||review.decision!=='accept'||review.decision_route!=='selection'||review.decision_hash!==intent.decision_hash)throw new Error('CULLING_DECISION_CONFLICT');return structuredClone(review);
}
export async function recoverCullingDecision(api:{review:(id:string,signal?:AbortSignal)=>Promise<Review>;decideCulling:(review:string,request:CullingDecisionRequest,key:string,signal:AbortSignal)=>Promise<unknown>},value:CullingDecisionIntent,key:string,signal:AbortSignal){
 const intent=readCullingDecisionIntent(value);if(!cullingUUID(key))return fail();signal.throwIfAborted();
 const prior=cullingDecisionReceipt(await api.review(intent.selection.review_id,signal),intent);signal.throwIfAborted();if(prior)return prior;
 try{await api.decideCulling(intent.selection.review_id,structuredClone(intent.request),key,signal);signal.throwIfAborted();}
 catch(error){signal.throwIfAborted();if(error instanceof APIError&&[401,403,404,410].includes(error.status))throw error;const receipt=cullingDecisionReceipt(await api.review(intent.selection.review_id,signal),intent);signal.throwIfAborted();if(receipt)return receipt;throw error;}
 const receipt=cullingDecisionReceipt(await api.review(intent.selection.review_id,signal),intent);signal.throwIfAborted();if(!receipt)throw new Error('CULLING_RECEIPT_UNAVAILABLE');return receipt;
}
