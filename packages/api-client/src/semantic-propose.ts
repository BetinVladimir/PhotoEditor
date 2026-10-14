import type {ApiClient} from './index';
export type SemanticIntentArtifactRef={artifact_id:string;sha256:string};
export type SemanticEditIntent={schema_version:1;kind:'remove_object'|'local_exposure'|'background_cleanup'|'retouch_region';reference:{asset_version_id:string;source:SemanticIntentArtifactRef;canvas_hash:string};reference_mask:SemanticIntentArtifactRef;target_snapshot_id:string;strength:number;ev:number|null;preserve_persons:true;prompt:string|null};
export type SemanticJobTicket={run_id:string;workflow_id:string;state:'queued'};
const record=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const exact=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const ref=(v:unknown)=>record(v)&&exact(v,['artifact_id','sha256'])&&id(v.artifact_id)&&sha(v.sha256);
export function decodeSemanticEditIntent(v:unknown):SemanticEditIntent {
 if(!record(v)||!exact(v,['schema_version','kind','reference','reference_mask','target_snapshot_id','strength','ev','preserve_persons','prompt'])||v.schema_version!==1||!['remove_object','local_exposure','background_cleanup','retouch_region'].includes(v.kind as string)||!record(v.reference)||!exact(v.reference,['asset_version_id','source','canvas_hash'])||!id(v.reference.asset_version_id)||!ref(v.reference.source)||!sha(v.reference.canvas_hash)||!ref(v.reference_mask)||!id(v.target_snapshot_id)||typeof v.strength!=='number'||!Number.isFinite(v.strength)||v.strength<0||v.strength>1||v.preserve_persons!==true||!(v.kind==='local_exposure'?typeof v.ev==='number'&&Number.isFinite(v.ev)&&v.ev>=-1&&v.ev<=1:v.ev===null)||!(v.prompt===null||typeof v.prompt==='string'&&!/[\uD800-\uDFFF]/.test(v.prompt.replace(/[\uD800-\uDBFF][\uDC00-\uDFFF]/g,''))&&Array.from(v.prompt).length>=1&&Array.from(v.prompt).length<=1024))throw new Error('SEMANTIC_PROPOSE_REQUEST_INVALID');
 return structuredClone(v) as SemanticEditIntent;
}
export function decodeSemanticJobTicket(v:unknown):SemanticJobTicket {
 if(!record(v)||!exact(v,['run_id','workflow_id','state'])||!id(v.run_id)||!id(v.workflow_id)||v.state!=='queued')throw new Error('SEMANTIC_PROPOSE_RESPONSE_INVALID');
 return structuredClone(v) as SemanticJobTicket;
}
export async function proposeSemanticIntent(client:Pick<ApiClient,'request'>,tenant:string,input:SemanticEditIntent,key:string,signal?:AbortSignal):Promise<SemanticJobTicket>{
 if(!id(tenant)||typeof key!=='string'||!/^[!-~]{1,128}$/.test(key))throw new Error('SEMANTIC_PROPOSE_REQUEST_INVALID');
 const body=decodeSemanticEditIntent(input);
 return decodeSemanticJobTicket(await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-intents`,{method:'POST',body,idempotencyKey:key,signal}));
}
