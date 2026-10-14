import type {ApiClient} from './index';

export type SignCreate={signing_policy_id:string};
export type SigningJobTicket={run_id:string;workflow_id:string;state:'queued'};
export type SigningPolicyChoice={id:string;revision:string};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const fields=(value:unknown,keys:string[]):value is Record<string,unknown>=>value!==null&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).length===keys.length&&keys.every(key=>Object.prototype.hasOwnProperty.call(value,key));

export function freezeSignCreate(value:SignCreate):SignCreate{
 if(!fields(value,['signing_policy_id'])||!uuid(value.signing_policy_id))throw new Error('SIGNING_REQUEST_INVALID');
 return {signing_policy_id:value.signing_policy_id};
}
export function readSigningTicket(value:unknown):SigningJobTicket{
 if(!fields(value,['run_id','workflow_id','state'])||!uuid(value.run_id)||!uuid(value.workflow_id)||value.run_id===value.workflow_id||value.state!=='queued')throw new Error('SIGNING_TICKET_INVALID');
 return {run_id:value.run_id,workflow_id:value.workflow_id,state:'queued'};
}

export function readSigningPolicies(value:unknown):SigningPolicyChoice[]{
 const invalid=():never=>{throw new Error('SIGNING_POLICIES_INVALID');};
 if(!fields(value,['items'])||!Array.isArray(value.items)||value.items.length>256)return invalid();
 const seen=new Set<string>();
 const result:SigningPolicyChoice[]=[];
 for(const item of value.items){
  if(!fields(item,['id','revision'])||!uuid(item.id)||seen.has(item.id)
    ||typeof item.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(item.revision)
    ||BigInt(item.revision)>9223372036854775807n)return invalid();
  seen.add(item.id);result.push({id:item.id,revision:item.revision});
 }
 return result;
}

/** Advisory choices only. Actual signing repeats current policy authority. */
export async function signingPolicies(client:Pick<ApiClient,'request'>,tenant:string,exportID:string,signal?:AbortSignal):Promise<SigningPolicyChoice[]>{
 if(!uuid(tenant)||!uuid(exportID))throw new Error('SIGNING_SCOPE_INVALID');
 signal?.throwIfAborted();
 const result=await client.request<unknown>(`/api/v1/tenants/${tenant}/exports/${exportID}/signing-policies`,{signal});
 signal?.throwIfAborted();
 return readSigningPolicies(result);
}

/** Caller retains this key/body for ambiguous retries; this never invents a new
 * key or treats a queued creation acknowledgement as successful signing. */
export async function signExport(client:Pick<ApiClient,'request'>,tenant:string,exportID:string,request:SignCreate,key:string,signal?:AbortSignal):Promise<SigningJobTicket>{
 if(!uuid(tenant)||!uuid(exportID)||typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key))throw new Error('SIGNING_SCOPE_INVALID');
 const body=freezeSignCreate(request);
 signal?.throwIfAborted();
 const result=await client.request<unknown>(`/api/v1/tenants/${tenant}/exports/${exportID}/provenance`,{method:'POST',body,idempotencyKey:key,signal});
 signal?.throwIfAborted();
 return readSigningTicket(result);
}
