import type {V2DraftResource} from './catalog-types';
import {scopeUUID} from './workspace-access';
const fields=(value:unknown,keys:readonly string[]):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).length===keys.length&&keys.every(key=>Object.hasOwn(value,key));
const revision=(value:unknown):value is string=>typeof value==='string'&&/^[1-9][0-9]{0,18}$/.test(value)&&BigInt(value)<=9223372036854775807n;
const ref=(value:unknown)=>fields(value,['artifact_id','sha256'])&&scopeUUID(value.artifact_id)&&typeof value.sha256==='string'&&/^[0-9a-f]{64}$/.test(value.sha256);
const timestamp=(value:unknown):value is string=>typeof value==='string'&&/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(value)&&Number.isFinite(Date.parse(value));
export function readV2DraftResource(value:unknown,tenant:string):V2DraftResource{
 if(!scopeUUID(tenant)||!fields(value,['id','revision','document','data'])||!scopeUUID(value.id)||!revision(value.revision)||!ref(value.document)||!fields(value.data,['resource_type','id','tenant_id','project_id','created_at','revision','updated_at','asset_version_id','stream_type','current_document_id','approved_document_id']))throw new Error('V2_DRAFT_RESPONSE_INVALID');
 const data=value.data;
 if(data.resource_type!=='v2_document_streams'||data.stream_type!=='draft'||data.id!==value.id||data.tenant_id!==tenant||!scopeUUID(data.project_id)||data.revision!==value.revision||!scopeUUID(data.asset_version_id)||!scopeUUID(data.current_document_id)||data.approved_document_id!==null&&!scopeUUID(data.approved_document_id)||!timestamp(data.created_at)||!timestamp(data.updated_at)||Date.parse(data.updated_at)<Date.parse(data.created_at))throw new Error('V2_DRAFT_RESPONSE_INVALID');
 return structuredClone(value) as V2DraftResource;
}

export function readV2DraftHistory(value:unknown,tenant:string):{items:import('./catalog-types').V2DocumentResource[];next_cursor:string|null}{
 if(!scopeUUID(tenant)||!fields(value,['items','next_cursor'])||!Array.isArray(value.items)||value.items.length>100||!(value.next_cursor===null||typeof value.next_cursor==='string'&&value.next_cursor.length>0&&value.next_cursor.length<=2048)||value.next_cursor!==null&&value.items.length===0)throw new Error('V2_HISTORY_RESPONSE_INVALID');
 let stream:string|undefined,last:bigint|undefined;const seen=new Set<string>();
 for(const item of value.items){
  if(!fields(item,['id','revision','document','data'])||!scopeUUID(item.id)||item.revision!=='1'||!ref(item.document)||!fields(item.data,['resource_type','id','tenant_id','project_id','created_at','stream_id','version_no','document_type','artifact_id','sha256']))throw new Error('V2_HISTORY_RESPONSE_INVALID');
  const data=item.data,document=item.document as Record<string,unknown>;
  if(data.resource_type!=='v2_documents'||data.id!==item.id||data.tenant_id!==tenant||!scopeUUID(data.project_id)||!scopeUUID(data.stream_id)||!revision(data.version_no)||data.document_type!=='EditingRecipeV3'||data.artifact_id!==document.artifact_id||data.sha256!==document.sha256||!timestamp(data.created_at)||seen.has(item.id)||stream!==undefined&&data.stream_id!==stream||last!==undefined&&BigInt(data.version_no)>=last)throw new Error('V2_HISTORY_RESPONSE_INVALID');
  stream=data.stream_id;last=BigInt(data.version_no);seen.add(item.id);
 }
 return structuredClone(value) as {items:import('./catalog-types').V2DocumentResource[];next_cursor:string|null};
}
