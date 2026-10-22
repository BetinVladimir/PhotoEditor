import type {Page,StyleVersion} from './catalog-types';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function object(v:unknown):Record<string,unknown>{if(!v||typeof v!=='object'||Array.isArray(v))throw Error('STYLE_VERSION_RESPONSE_INVALID');return v as Record<string,unknown>;}
export function readStyleVersionPage(raw:unknown,style:string,cursor?:string):Page<StyleVersion>{
 const page=object(raw);
 if(!uuid(style)||Object.keys(page).some(k=>!['items','next_cursor'].includes(k))||!Array.isArray(page.items)||page.items.length>100||page.next_cursor!==null&&(typeof page.next_cursor!=='string'||!page.next_cursor||page.next_cursor.length>4096||page.next_cursor===cursor||page.items.length===0))throw Error('STYLE_VERSION_PAGE_INVALID');
 const items=page.items.map(raw=>{
  const v=object(raw);
  if(Object.keys(v).some(k=>!['id','style_id','version_no','config','config_hash','schema_version','renderer_version','training_run_id','approval_status','metrics','export_artifact_id','source_file_sha256','created_at'].includes(k))||!uuid(v.id)||v.style_id!==style||!Number.isSafeInteger(v.version_no)||(v.version_no as number)<1||(v.version_no as number)>2147483647||!sha(v.config_hash)||v.schema_version!=='2.0.0'||v.renderer_version!=='photo_renderer_v2'||!['accepted','needs_review'].includes(v.approval_status as string)||v.training_run_id!==undefined&&!uuid(v.training_run_id)||v.export_artifact_id!==undefined&&!uuid(v.export_artifact_id)||v.source_file_sha256!==undefined&&!sha(v.source_file_sha256)||typeof v.created_at!=='string'||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(v.created_at)||!Number.isFinite(Date.parse(v.created_at)))throw Error('STYLE_VERSION_RESPONSE_INVALID');
  object(v.config);object(v.metrics);
  return structuredClone(v) as StyleVersion;
 });
 if(new Set(items.map(v=>v.id)).size!==items.length||new Set(items.map(v=>v.version_no)).size!==items.length||items.some((v,i)=>i>0&&items[i-1].version_no>=v.version_no))throw Error('STYLE_VERSION_PAGE_INVALID');
 return {items,next_cursor:page.next_cursor as string|null};
}
