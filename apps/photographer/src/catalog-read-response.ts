import type {Page,Project,StyleProfile} from './catalog-types';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n;
function object(v:unknown):Record<string,unknown>{if(!v||typeof v!=='object'||Array.isArray(v))throw Error('CATALOG_RESPONSE_INVALID');return v as Record<string,unknown>;}
function common(raw:unknown,tenant:string,keys:string[]){const v=object(raw);if(!uuid(tenant)||v.tenant_id!==tenant||!uuid(v.id)||Object.keys(v).some(k=>!keys.includes(k))||typeof v.name!=='string'||!v.name.trim()||Array.from(v.name).length>120||typeof v.description!=='string'||Array.from(v.description).length>2000||!revision(v.revision)||typeof v.created_at!=='string'||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(v.created_at)||!Number.isFinite(Date.parse(v.created_at)))throw Error('CATALOG_RESPONSE_INVALID');return v;}
export function readProject(raw:unknown,tenant:string,id?:string,requireAccess=false):Project{
 const v=common(raw,tenant,['id','tenant_id','name','description','visibility','download_allowed','revision','created_at','can_edit']);
 if(id!==undefined&&v.id!==id||!['workspace','restricted'].includes(v.visibility as string)||typeof v.download_allowed!=='boolean'||v.can_edit!==undefined&&typeof v.can_edit!=='boolean'||requireAccess&&typeof v.can_edit!=='boolean')throw Error('PROJECT_RESPONSE_INVALID');return {...v} as Project;
}
export function readStyleProfile(raw:unknown,tenant:string,scope?:{id?:string;project?:string}):StyleProfile{
 const v=common(raw,tenant,['id','tenant_id','project_id','name','description','archived','revision','created_at']);
 if(!uuid(v.project_id)||typeof v.archived!=='boolean'||scope?.id!==undefined&&v.id!==scope.id||scope?.project!==undefined&&v.project_id!==scope.project)throw Error('STYLE_RESPONSE_INVALID');return {...v} as StyleProfile;
}
function page<T extends{id:string}>(raw:unknown,decode:(v:unknown)=>T,requestedCursor?:string):Page<T>{
 const v=object(raw);if(Object.keys(v).some(k=>!['items','next_cursor','revision'].includes(k))||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==null&&(typeof v.next_cursor!=='string'||!v.next_cursor||v.next_cursor.length>4096||v.next_cursor===requestedCursor||v.items.length===0)||v.revision!==undefined&&!revision(v.revision))throw Error('CATALOG_PAGE_INVALID');
 const items=v.items.map(decode);if(new Set(items.map(x=>x.id)).size!==items.length)throw Error('CATALOG_PAGE_INVALID');return {items,next_cursor:v.next_cursor as string|null,...(v.revision===undefined?{}:{revision:v.revision as string})};
}
export function readProjectPage(raw:unknown,tenant:string,cursor?:string){return page(raw,v=>readProject(v,tenant),cursor);}
export function readStylePage(raw:unknown,tenant:string,project?:string,cursor?:string){if(project!==undefined&&!uuid(project))throw Error('STYLE_SCOPE_INVALID');return page(raw,v=>readStyleProfile(v,tenant,{project}),cursor);}
