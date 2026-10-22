import type {CaptureShotListView,CaptureShotRequirement} from './capture-types';
import {canonicalJSON} from '@photoeditor/api-client';
import {v2ResourceData} from './v2-resource';
import type {CapturePerson} from './capture-types';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n;
function object(v:unknown):Record<string,unknown>{if(!v||typeof v!=='object'||Array.isArray(v))throw Error('CAPTURE_SHOTS_INVALID');return v as Record<string,unknown>;}
export function freezeCaptureShots(raw:unknown):CaptureShotRequirement[]{
 if(!Array.isArray(raw)||raw.length>200)throw Error('CAPTURE_SHOTS_INVALID');
 const ids=new Set<string>();return raw.map(item=>{const v=object(item);
  if(Object.keys(v).sort().join(',')!=='id,label,min_confirmed_frames,min_quality,person_ids'||!uuid(v.id)||ids.has(v.id)||typeof v.label!=='string'||Array.from(v.label).length<1||Array.from(v.label).length>200||!Array.isArray(v.person_ids)||v.person_ids.length>100||!v.person_ids.every(uuid)||new Set(v.person_ids).size!==v.person_ids.length||!Number.isInteger(v.min_confirmed_frames)||(v.min_confirmed_frames as number)<1||(v.min_confirmed_frames as number)>100||typeof v.min_quality!=='number'||!Number.isFinite(v.min_quality)||v.min_quality<0||v.min_quality>1)throw Error('CAPTURE_SHOTS_INVALID');
  ids.add(v.id);return structuredClone(v) as unknown as CaptureShotRequirement;
 });
}
export function readCaptureShots(raw:unknown,session:string):CaptureShotListView{
 const v=object(raw);if(!uuid(session)||v.session_id!==session||Object.keys(v).sort().join(',')!=='document,requirements,revision,session_id'||!revision(v.revision))throw Error('CAPTURE_SHOT_RESPONSE_INVALID');
 const requirements=freezeCaptureShots(v.requirements);
 if(v.document===null){if(requirements.length)throw Error('CAPTURE_SHOT_RESPONSE_INVALID');}
 else{const ref=object(v.document);if(Object.keys(ref).sort().join(',')!=='artifact_id,sha256'||!uuid(ref.artifact_id)||typeof ref.sha256!=='string'||!(/^[0-9a-f]{64}$/).test(ref.sha256))throw Error('CAPTURE_SHOT_RESPONSE_INVALID');}
 return {session_id:session,revision:v.revision,document:structuredClone(v.document) as CaptureShotListView['document'],requirements};
}
export function readCaptureShotsReplacement(raw:unknown,session:string,before:string,requirements:CaptureShotRequirement[]){
 const value=readCaptureShots(raw,session);if(!revision(before)||BigInt(value.revision)!==BigInt(before)+1n||value.document===null||canonicalJSON(value.requirements)!==canonicalJSON(requirements))throw Error('CAPTURE_SHOT_RESPONSE_INVALID');return value;
}
export function readCapturePeople(raw:unknown,tenant:string,project:string,cursor?:string):{items:CapturePerson[];next_cursor:string|null}{
 const page=object(raw);if(!uuid(tenant)||!uuid(project)||!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===null||(typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=2048&&page.next_cursor!==cursor)))throw Error('CAPTURE_PEOPLE_INVALID');
 const ids=new Set<string>();const items=page.items.map(item=>{const v=v2ResourceData<Record<string,unknown>>(item,'project_persons');if(!uuid(v.id)||ids.has(v.id)||v.tenant_id!==tenant||v.project_id!==project||!revision(v.revision)||!revision(v.consent_epoch)||typeof v.label!=='string'||Array.from(v.label).length<1||Array.from(v.label).length>256||(v.state!=='active'&&v.state!=='revoked'))throw Error('CAPTURE_PEOPLE_INVALID');ids.add(v.id);return {id:v.id,label:v.label,state:v.state} as CapturePerson;});
 return {items,next_cursor:page.next_cursor as string|null};
}
