import {canonicalJSON} from '@photoeditor/api-client';
import {scopeUUID} from './workspace-access';
import type {ShootSession,ShootSessionCreate} from './capture-types';
const fields=(v:unknown,keys:string[])=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===[...keys].sort().join(',');
const integer=(v:unknown,min:number,max:number)=>Number.isSafeInteger(v)&&(v as number)>=min&&(v as number)<=max;
const ref=(v:any)=>fields(v,['artifact_id','sha256'])&&scopeUUID(v.artifact_id)&&typeof v.sha256==='string'&&/^[0-9a-f]{64}$/.test(v.sha256);
export function freezeCaptureCreate(v:ShootSessionCreate):ShootSessionCreate{
 const fail=():never=>{throw new Error('CAPTURE_CREATE_INVALID')};
 if(!fields(v,['project_id','collection_id','name','policy','capture_policy'])||!scopeUUID(v.project_id)||!scopeUUID(v.collection_id)||typeof v.name!=='string'||!v.name.trim()||v.name!==v.name.trim()||[...v.name].length>200)return fail();
 const p=v.policy,c=v.capture_policy;
 if(!fields(p,['schema_version','time_gap_seconds','neighbor_count','protect_manual_picks','camera_offsets','calibration'])||p.schema_version!==1||!integer(p.time_gap_seconds,30,7200)||!integer(p.neighbor_count,1,64)||p.protect_manual_picks!==true||!ref(p.calibration)||!Array.isArray(p.camera_offsets)||p.camera_offsets.length>32||p.camera_offsets.some(x=>!fields(x,['camera_hash','seconds'])||!ref({artifact_id:v.project_id,sha256:x.camera_hash})||!integer(x.seconds,-86400,86400))||new Set(p.camera_offsets.map(x=>x.camera_hash)).size!==p.camera_offsets.length)return fail();
 if(!c||!fields(c,['schema_version','checks','max_queue_age_seconds','calibration'])||c.schema_version!==1||c.max_queue_age_seconds!==30||!ref(c.calibration)||!Array.isArray(c.checks)||c.checks.length<1||c.checks.length>6||new Set(c.checks).size!==c.checks.length||c.checks.some(x=>!['focus','eyes','clipping','glare','duplicate','shot_list'].includes(x)))return fail();return structuredClone(v);
}
export function readCreatedCapture(value:ShootSession,request:ShootSessionCreate,id?:string,initial=false):ShootSession{
 if(!value||!scopeUUID(value.id)||id!==undefined&&value.id!==id||value.project_id!==request.project_id||value.collection_id!==request.collection_id||typeof value.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n||!['open','closed','archived'].includes(value.state))throw new Error('CAPTURE_CREATE_RECEIPT_INVALID');
 if(value.capture_policy===null||value.capture_policy===undefined)throw new Error('CAPTURE_CREATE_RECEIPT_INVALID');if(initial&&(value.name!==request.name||canonicalJSON(value.policy)!==canonicalJSON(request.policy)||canonicalJSON(value.capture_policy)!==canonicalJSON(request.capture_policy)))throw new Error('CAPTURE_CREATE_RECEIPT_INVALID');
 if(typeof value.name!=='string'||[...value.name].length<1||[...value.name].length>200)throw new Error('CAPTURE_CREATE_RECEIPT_INVALID');freezeCaptureCreate({project_id:value.project_id,collection_id:value.collection_id,name:'validated',policy:value.policy,capture_policy:value.capture_policy});return structuredClone(value);
}
