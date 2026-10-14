import type {ApiClient} from './index';
export type RunOutput={artifact_id:string;role:string;sha256:string;bytes:number;media_type:string;url:string;expires_at:string};
export type RunOutputPage={items:RunOutput[]};
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const record=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
export function decodeRunOutputs(value:unknown):RunOutputPage {
 const fail=():never=>{throw new Error('RUN_OUTPUT_RESPONSE_INVALID')};
 if(!record(value)||Object.keys(value).length!==1||!Array.isArray(value.items))return fail();
 const seen=new Set<string>();
 for(const output of value.items){
  if(!record(output)||Object.keys(output).length!==7||!['artifact_id','role','sha256','bytes','media_type','url','expires_at'].every(k=>Object.hasOwn(output,k)))return fail();
  if(!id(output.artifact_id)||seen.has(output.artifact_id)||typeof output.sha256!=='string'||!/^[0-9a-f]{64}$/.test(output.sha256)||!Number.isSafeInteger(output.bytes)||Number(output.bytes)<1||typeof output.role!=='string'||!/^[a-z][a-z0-9_]{0,79}$/.test(output.role)||typeof output.media_type!=='string'||!/^[a-z0-9.+-]+\/[a-z0-9.+-]+$/.test(output.media_type)||typeof output.url!=='string'||output.url.length>16384||typeof output.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T/.test(output.expires_at)||!Number.isFinite(Date.parse(output.expires_at)))return fail();
  seen.add(output.artifact_id);
  let url:URL;try{url=new URL(output.url)}catch{return fail()}
  if(url.username||url.password||url.hash||!(url.protocol==='https:'||url.protocol==='http:'&&['localhost','127.0.0.1','[::1]'].includes(url.hostname)))return fail();
 }
 return structuredClone(value) as RunOutputPage;
}
export async function listRunOutputs(client:Pick<ApiClient,'request'>,tenant:string,run:string,signal?:AbortSignal):Promise<RunOutputPage>{
 if(!id(tenant)||!id(run))throw new Error('RUN_OUTPUT_QUERY_INVALID');
 return decodeRunOutputs(await client.request<unknown>(`/api/v1/tenants/${tenant}/runs/${run}/outputs`,{signal}));
}
