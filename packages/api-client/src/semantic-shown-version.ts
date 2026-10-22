import type {ApiClient} from './index';
export type SemanticShownVersionResult={asset_version_id:string;asset_id:string};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
// Target-derived server identity makes retries return the same version.
export async function materializeSemanticShownVersion(client:Pick<ApiClient,'request'>,tenant:string,target:string,signal?:AbortSignal):Promise<SemanticShownVersionResult>{
 if(!uuid(tenant)||!uuid(target))throw new Error('SEMANTIC_SHOWN_VERSION_REQUEST_INVALID');
 signal?.throwIfAborted();
 const value=await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-targets/${target}/version`,{method:'POST',signal});
 signal?.throwIfAborted();
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).sort().join(',')!=='asset_id,asset_version_id'||!('asset_version_id' in value)||!uuid(value.asset_version_id)||!('asset_id' in value)||!uuid(value.asset_id))throw new Error('SEMANTIC_SHOWN_VERSION_RESPONSE_INVALID');
 return {asset_version_id:value.asset_version_id,asset_id:value.asset_id};
}
