export type DeliveryMetadataField='author'|'copyright'|'camera_make'|'camera_model'|'lens'|'datetime_original'|'gps';
export type DeliveryCreateRequest={project_id:string;snapshot_id:string;name:string;policy:{required_presets:string[];optional_presets:string[];require_review:boolean;metadata_policy:'strip'|'copyright'|'approved_exif';approved_fields?:DeliveryMetadataField[];provenance:'off'|'required';signing_policy_id?:string;signing_policy_sha?:string;allow_omitted:boolean}};
export type DeliveryCreateTicket={run_id:string;workflow_id:string;state:'queued'};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const invalid=():never=>{throw new Error('DELIVERY_CREATE_INVALID');};
export function freezeDeliveryCreate(value:DeliveryCreateRequest,key:string):DeliveryCreateRequest{
 if(typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key)||!fields(value,['project_id','snapshot_id','name','policy'])||!uuid(value.project_id)||!uuid(value.snapshot_id)||typeof value.name!=='string'||Array.from(value.name).length<1||Array.from(value.name).length>200)return invalid();
 const p=value.policy;
 const policyKeys=['required_presets','optional_presets','require_review','metadata_policy','provenance','allow_omitted'];
 if(p&&Object.prototype.hasOwnProperty.call(p,'approved_fields'))policyKeys.push('approved_fields');
 if(p&&Object.prototype.hasOwnProperty.call(p,'signing_policy_id'))policyKeys.push('signing_policy_id');
 if(p&&Object.prototype.hasOwnProperty.call(p,'signing_policy_sha'))policyKeys.push('signing_policy_sha');
 if(!fields(p,policyKeys)||!Array.isArray(p.required_presets)||p.required_presets.length<1||p.required_presets.length>16||!Array.isArray(p.optional_presets)||p.optional_presets.length>16||p.required_presets.length+p.optional_presets.length>16||typeof p.require_review!=='boolean'||typeof p.allow_omitted!=='boolean'||!['strip','copyright','approved_exif'].includes(p.metadata_policy as string)||!['off','required'].includes(p.provenance as string))return invalid();
 const ids=[...p.required_presets,...p.optional_presets];
 if(!ids.every(uuid)||new Set(ids).size!==ids.length)return invalid();
 const approved=p.approved_fields;
 if(approved!==undefined&&(!Array.isArray(approved)||approved.length>7||new Set(approved).size!==approved.length||approved.some(v=>!['author','copyright','camera_make','camera_model','lens','datetime_original','gps'].includes(v))||p.metadata_policy!=='approved_exif'&&approved.length>0))return invalid();
 const signerIDPresent=Object.prototype.hasOwnProperty.call(p,'signing_policy_id');
 const signerSHAPresent=Object.prototype.hasOwnProperty.call(p,'signing_policy_sha');
 if(p.provenance==='off'&&(signerIDPresent||signerSHAPresent))return invalid();
 if(p.provenance==='required'&&(!signerIDPresent||!signerSHAPresent||!uuid(p.signing_policy_id)||typeof p.signing_policy_sha!=='string'||!/^[0-9a-f]{64}$/.test(p.signing_policy_sha)))return invalid();
 const owned=structuredClone(value);
 if(approved?.length)owned.policy.approved_fields=[...approved].sort();else delete owned.policy.approved_fields;
 return owned;
}
export function readDeliveryCreateTicket(value:unknown):DeliveryCreateTicket{
 if(!fields(value,['run_id','workflow_id','state'])||!uuid(value.run_id)||!uuid(value.workflow_id)||value.state!=='queued')return invalid();
 return {run_id:value.run_id,workflow_id:value.workflow_id,state:'queued'};
}
