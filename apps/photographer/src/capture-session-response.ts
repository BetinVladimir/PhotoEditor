import type {Page} from './catalog-types';
import type {ShootSession} from './capture-types';
import type {ShootSessionUpdate} from './capture-types';
import {canonicalJSON} from '@photoeditor/api-client';
import {scopeUUID} from './workspace-access';
import {v2ResourceData} from './v2-resource';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>object(v)&&Object.keys(v).sort().join(',')===[...keys].sort().join(',');
const integer=(v:unknown,min:number,max:number)=>Number.isSafeInteger(v)&&Number(v)>=min&&Number(v)<=max;
const revision=(v:unknown)=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n;
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const ref=(v:unknown)=>fields(v,['artifact_id','sha256'])&&scopeUUID(v.artifact_id)&&sha(v.sha256);
const time=(v:unknown)=>typeof v==='string'&&/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(v)&&Number.isFinite(Date.parse(v));
function policies(p:unknown,c:unknown){
 if(!fields(p,['schema_version','time_gap_seconds','neighbor_count','protect_manual_picks','camera_offsets','calibration'])||p.schema_version!==1||!integer(p.time_gap_seconds,30,7200)||!integer(p.neighbor_count,1,64)||p.protect_manual_picks!==true||!ref(p.calibration)||!Array.isArray(p.camera_offsets)||p.camera_offsets.length>32||p.camera_offsets.some(x=>!fields(x,['camera_hash','seconds'])||!sha(x.camera_hash)||!integer(x.seconds,-86400,86400))||new Set(p.camera_offsets.map(x=>x.camera_hash)).size!==p.camera_offsets.length)throw Error('CAPTURE_SESSION_POLICY_INVALID');
 // Sessions created before capture assistance remain valid readonly/template
 // records. Null is the actual nullable database/DTO contract, not {}.
 if(c===null)return;
 if(!fields(c,['schema_version','checks','max_queue_age_seconds','calibration'])||c.schema_version!==1||c.max_queue_age_seconds!==30||!ref(c.calibration)||!Array.isArray(c.checks)||c.checks.length<1||c.checks.length>6||new Set(c.checks).size!==c.checks.length||c.checks.some(x=>!['focus','eyes','clipping','glare','duplicate','shot_list'].includes(x)))throw Error('CAPTURE_SESSION_POLICY_INVALID');
}
export function readShootSession(value:unknown,tenant:string,scope?:{id?:string;project?:string;collection?:string}):ShootSession{
 const raw=v2ResourceData<unknown>(value,'shoot_sessions',scope?.id);
 if(!fields(raw,['resource_type','id','tenant_id','project_id','created_at','revision','updated_at','collection_id','name','state','policy','capture_policy','consent_epoch'])||!scopeUUID(tenant)||raw.tenant_id!==tenant||!scopeUUID(raw.id)||!scopeUUID(raw.project_id)||!scopeUUID(raw.collection_id)||scope?.project!==undefined&&raw.project_id!==scope.project||scope?.collection!==undefined&&raw.collection_id!==scope.collection||!revision(raw.revision)||!revision(raw.consent_epoch)||!time(raw.created_at)||!time(raw.updated_at)||typeof raw.name!=='string'||[...raw.name].length<1||[...raw.name].length>200||!['open','closed','archived'].includes(raw.state as string))throw Error('CAPTURE_SESSION_RESPONSE_INVALID');
 policies(raw.policy,raw.capture_policy);return structuredClone(raw) as unknown as ShootSession;
}
export function readShootSessionPage(value:unknown,tenant:string,project:string,cursor?:string):Page<ShootSession>{
 if(!object(value)||!scopeUUID(project)||Object.keys(value).some(k=>!['items','next_cursor','revision'].includes(k))||!Array.isArray(value.items)||value.items.length>100||value.next_cursor!==null&&(typeof value.next_cursor!=='string'||!value.next_cursor||value.next_cursor.length>4096||value.next_cursor===cursor||!value.items.length)||value.revision!==undefined&&!revision(value.revision))throw Error('CAPTURE_SESSION_PAGE_INVALID');
 const items=value.items.map(v=>readShootSession(v,tenant,{project}));if(new Set(items.map(x=>x.id)).size!==items.length)throw Error('CAPTURE_SESSION_PAGE_INVALID');return {items,next_cursor:value.next_cursor as string|null,...(value.revision===undefined?{}:{revision:value.revision as string})};
}
export function readShootSessionUpdate(value:unknown,tenant:string,id:string,request:ShootSessionUpdate):ShootSession{
 const row=readShootSession(value,tenant,{id});
 if(!revision(request.expected_revision)||BigInt(row.revision)!==BigInt(request.expected_revision)+1n||row.name!==request.name||row.state!==request.state||canonicalJSON(row.policy)!==canonicalJSON(request.policy)||canonicalJSON(row.capture_policy)!==canonicalJSON(request.capture_policy))throw Error('CAPTURE_SESSION_CAS_RECEIPT_INVALID');return row;
}
