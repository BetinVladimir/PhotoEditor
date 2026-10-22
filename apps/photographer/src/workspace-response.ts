/** Current tenant.Service Tenant projection. A type assertion is not receipt proof. */
export type WorkspaceTenant={id:string;name:string;role:'owner'|'admin'|'editor'|'viewer';revision:string};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
function object(v:unknown):Record<string,unknown>{if(!v||typeof v!=='object'||Array.isArray(v))throw new Error('WORKSPACE_RESPONSE_INVALID');return v as Record<string,unknown>;}
function policy(v:unknown,keys:string[]){const o=object(v);if(Object.keys(o).sort().join(',')!==[...keys].sort().join(',')||keys.some(key=>!Number.isSafeInteger(o[key])||(o[key] as number)<0))throw new Error('WORKSPACE_RESPONSE_INVALID');}
export function readWorkspaceTenant(value:unknown,expected?:{id?:string;createdName?:string}):WorkspaceTenant{
 const v=object(value);
 if(Object.keys(v).some(k=>!['id','name','role','revision','quotas','retention'].includes(k))||!uuid(v.id)||expected?.id!==undefined&&v.id!==expected.id||typeof v.name!=='string'||!v.name.trim()||Array.from(v.name).length>120||expected?.createdName!==undefined&&(v.name!==expected.createdName||v.role!=='owner')||!['owner','admin','editor','viewer'].includes(v.role as string)||typeof v.revision!=='string'||! /^[1-9][0-9]{0,18}$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n)throw new Error('WORKSPACE_RESPONSE_INVALID');
 if(v.quotas!==undefined)policy(v.quotas,['storage_bytes','source_files','max_active_gpu_tasks']);
 if(v.retention!==undefined)policy(v.retention,['processing_days','exports_days','temporary_hours']);
 return {id:v.id,name:v.name,role:v.role as WorkspaceTenant['role'],revision:v.revision};
}
export function readWorkspacePage(value:unknown):{items:WorkspaceTenant[];next_cursor?:string|null}{
 const v=object(value);
 if(Object.keys(v).some(k=>!['items','next_cursor'].includes(k))||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==undefined&&v.next_cursor!==null&&(typeof v.next_cursor!=='string'||!v.next_cursor||v.next_cursor.length>4096))throw new Error('WORKSPACE_RESPONSE_INVALID');
 const items=v.items.map(item=>readWorkspaceTenant(item));if(new Set(items.map(item=>item.id)).size!==items.length)throw new Error('WORKSPACE_RESPONSE_INVALID');
 return {items,next_cursor:v.next_cursor as string|null|undefined};
}
