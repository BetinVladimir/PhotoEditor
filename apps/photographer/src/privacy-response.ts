const MAX_REVISION=9223372036854775807n;
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===[...keys].sort().join(',');
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export function privacyRevision(v:unknown):string{if(typeof v!=='string'||!/^[1-9][0-9]{0,18}$/.test(v)||BigInt(v)>MAX_REVISION)throw new Error('PRIVACY_REVISION_INVALID');return v;}
export function privacyMutationKey(v:unknown):string{if(typeof v!=='string'||!/^[!-~]{1,128}$/.test(v))throw new Error('PRIVACY_KEY_INVALID');return v;}
export type PrivacyPreferences={revision:string;analytics:boolean;marketing:boolean;updated_at:string};
export type PrivacyPreferencesUpdate={analytics?:boolean;marketing?:boolean};
export function privacyUpdateBody(value:PrivacyPreferencesUpdate):PrivacyPreferencesUpdate{
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length<1||Object.keys(value).some(k=>!['analytics','marketing'].includes(k))||Object.values(value).some(v=>typeof v!=='boolean'))throw new Error('PRIVACY_UPDATE_INVALID');
 return {...value};
}
export function readPrivacyPreferences(raw:unknown):PrivacyPreferences{
 if(!fields(raw,['revision','analytics','marketing','updated_at'])||typeof raw.analytics!=='boolean'||typeof raw.marketing!=='boolean'||typeof raw.updated_at!=='string'||!/^\d{4}-\d\d-\d\dT.+(?:Z|\+00:00)$/.test(raw.updated_at)||!Number.isFinite(Date.parse(raw.updated_at)))throw new Error('PRIVACY_RESPONSE_INVALID');
 return {revision:privacyRevision(raw.revision),analytics:raw.analytics,marketing:raw.marketing,updated_at:raw.updated_at};
}
export type DeletionOwnershipChoice={tenant_id:string;tenant_revision:string;allowed_actions:Array<'close'|'transfer'>};
export type DeletionPreflight={privacy_case_id:string;revision:string;ownership_choices:DeletionOwnershipChoice[];required_recent_auth_purpose:'delete_profile'};
export function readDeletionPreflight(raw:unknown):DeletionPreflight{
 if(!fields(raw,['privacy_case_id','revision','ownership_choices','required_recent_auth_purpose'])||!uuid(raw.privacy_case_id)||!Array.isArray(raw.ownership_choices)||raw.required_recent_auth_purpose!=='delete_profile')throw new Error('DELETION_PREFLIGHT_INVALID');
 const seen=new Set<string>();
 const choices=raw.ownership_choices.map((value:unknown):DeletionOwnershipChoice=>{
  if(!fields(value,['tenant_id','tenant_revision','allowed_actions'])||!uuid(value.tenant_id)||seen.has(value.tenant_id)||!Array.isArray(value.allowed_actions)||!value.allowed_actions.length||value.allowed_actions.some(v=>v!=='close'&&v!=='transfer')||new Set(value.allowed_actions).size!==value.allowed_actions.length)throw new Error('DELETION_PREFLIGHT_INVALID');
  seen.add(value.tenant_id);return {tenant_id:value.tenant_id,tenant_revision:privacyRevision(value.tenant_revision),allowed_actions:[...value.allowed_actions] as Array<'close'|'transfer'>};
 });
 return {privacy_case_id:raw.privacy_case_id,revision:privacyRevision(raw.revision),ownership_choices:choices,required_recent_auth_purpose:'delete_profile'};
}
