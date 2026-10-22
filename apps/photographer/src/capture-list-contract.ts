import {scopeUUID} from './workspace-access';
import {readCreatedCollection} from './collection-create-response';
import type {Page} from './catalog-types';
import type {ShootSession} from './capture-types';
function page(value:unknown):{items:unknown[];next_cursor:string|null}{
 if(!value||typeof value!=='object'||Array.isArray(value))throw Error('CAPTURE_PAGE_INVALID');const v=value as Record<string,unknown>;
 if(!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==undefined&&v.next_cursor!==null&&(typeof v.next_cursor!=='string'||!v.next_cursor.length||v.next_cursor.length>2048))throw Error('CAPTURE_PAGE_INVALID');
 return {items:v.items,next_cursor:typeof v.next_cursor==='string'?v.next_cursor:null};
}
export function readCaptureSessions(value:unknown,tenant:string,project:string):Page<ShootSession>{
 if(!scopeUUID(tenant)||!scopeUUID(project))throw Error('CAPTURE_PAGE_SCOPE_INVALID');const raw=page(value),seen=new Set<string>();
 const items=raw.items.map(item=>{if(!item||typeof item!=='object'||Array.isArray(item))throw Error('CAPTURE_SESSION_INVALID');const v=item as Record<string,unknown>;
 if(!scopeUUID(v.id)||seen.has(v.id)||v.tenant_id!==tenant||v.project_id!==project||typeof v.name!=='string'||!v.name.trim()||[...v.name].length>200||typeof v.state!=='string'||!['open','closed','archived'].includes(v.state)||typeof v.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n)throw Error('CAPTURE_SESSION_INVALID');seen.add(v.id);return structuredClone(v) as unknown as ShootSession;});
 return {items,next_cursor:raw.next_cursor};
}
export function readCaptureCollections(value:unknown,tenant:string,project:string){
 if(!scopeUUID(tenant)||!scopeUUID(project))throw Error('CAPTURE_PAGE_SCOPE_INVALID');const raw=page(value),seen=new Set<string>();
 const items=raw.items.map(item=>{const row=readCreatedCollection(item,tenant,project);if(seen.has(row.id))throw Error('CAPTURE_PAGE_INVALID');seen.add(row.id);return row;});
 return {items:items.filter(item=>item.state==='draft'||item.state==='published'),next_cursor:raw.next_cursor};
}
