import type {ApiClient} from '@photoeditor/api-client';
import type {Collection} from './catalog-types';
import {linkUUID} from './collection-access-links-api';
import {readCreatedCollection} from './collection-create-response';
export type CollectionMetadataPatch={name:string;description:string;sort_mode:'manual'|'captured_at'|'filename';cover_item_id:string|null};
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&(v.length<19||v<='9223372036854775807');
export class CollectionMetadataAPI{
 private root:string;
 constructor(private client:Pick<ApiClient,'request'>,private tenant:string,private project:string,private id:string){if(![tenant,project,id].every(linkUUID))throw Error('COLLECTION_SCOPE_INVALID');this.root=`/api/v1/tenants/${tenant}/collections/${id}`;}
 async get(signal:AbortSignal){signal.throwIfAborted();const raw=await this.client.request<unknown>(this.root,{signal});signal.throwIfAborted();const row=readCreatedCollection(raw,this.tenant,this.project);if(row.id!==this.id)throw Error('COLLECTION_INVALID');return row;}
 async update(current:Collection,patch:CollectionMetadataPatch,itemIDs:readonly string[],signal:AbortSignal){
  signal.throwIfAborted();if(current.id!==this.id||current.tenant_id!==this.tenant||current.project_id!==this.project||!revision(current.revision)||!['draft','published'].includes(current.state)||typeof patch.name!=='string'||!patch.name.trim()||Array.from(patch.name.trim()).length>120||typeof patch.description!=='string'||Array.from(patch.description).length>2000||!['manual','captured_at','filename'].includes(patch.sort_mode)||patch.cover_item_id!==null&&(!linkUUID(patch.cover_item_id)||!itemIDs.includes(patch.cover_item_id)))throw Error('COLLECTION_PATCH_INVALID');
  const frozen={name:patch.name.trim(),description:patch.description,sort_mode:patch.sort_mode,cover_item_id:patch.cover_item_id},expected=current.revision,originalState=current.state;
  const raw=await this.client.request<unknown>(this.root,{method:'PATCH',headers:{'If-Match':`"${expected}"`},body:frozen,signal});signal.throwIfAborted();const row=readCreatedCollection(raw,this.tenant,this.project);
  if(row.id!==this.id||row.name!==frozen.name||row.description!==frozen.description||row.sort_mode!==frozen.sort_mode||(row.cover_item_id??null)!==frozen.cover_item_id||row.state!==originalState||BigInt(row.revision)!==BigInt(expected)+1n)throw Error('COLLECTION_PATCH_RECEIPT_INVALID');return row;
 }
 async publishCollection(current:Collection,publish:boolean,signal:AbortSignal){
  signal.throwIfAborted();if(current.id!==this.id||current.tenant_id!==this.tenant||current.project_id!==this.project||!revision(current.revision)||!Number.isSafeInteger(Number(current.revision))||!['draft','published'].includes(current.state)||typeof publish!=='boolean')throw Error('COLLECTION_PUBLICATION_INVALID');
  const expected=current.revision,unchanged={name:current.name,description:current.description,sort_mode:current.sort_mode,cover_item_id:current.cover_item_id??null};
  const raw=await this.client.request<unknown>(`${this.root}/publication`,{method:'POST',body:{expected_revision:Number(expected),item_ids:[],publish,publish_collection:true},signal});signal.throwIfAborted();
  const row=readCreatedCollection(raw,this.tenant,this.project);if(row.id!==this.id||row.state!==(publish?'published':'draft')||row.name!==unchanged.name||row.description!==unchanged.description||row.sort_mode!==unchanged.sort_mode||(row.cover_item_id??null)!==unchanged.cover_item_id||BigInt(row.revision)!==BigInt(expected)+1n)throw Error('COLLECTION_PUBLICATION_RECEIPT_INVALID');return row;
 }
 async archive(current:Collection,signal:AbortSignal){
  signal.throwIfAborted();if(current.id!==this.id||current.tenant_id!==this.tenant||current.project_id!==this.project||!revision(current.revision)||!Number.isSafeInteger(Number(current.revision))||!['draft','published'].includes(current.state))throw Error('COLLECTION_DELETE_INVALID');
  const raw=await this.client.request<unknown>(this.root,{method:'DELETE',headers:{'If-Match':`"${current.revision}"`},body:{expected_revision:Number(current.revision),confirm_resource_id:this.id},signal});signal.throwIfAborted();
  if(!raw||typeof raw!=='object'||Array.isArray(raw)||Object.keys(raw).sort().join(',')!=='job_id,status'||!linkUUID((raw as any).job_id)||(raw as any).status!=='queued')throw Error('COLLECTION_DELETE_RECEIPT_INVALID');return {job_id:(raw as {job_id:string}).job_id,status:'queued' as const};
 }
}
