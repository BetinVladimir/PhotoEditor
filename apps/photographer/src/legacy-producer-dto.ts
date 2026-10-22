import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {scopeUUID} from './workspace-access';
import type {RunAccepted,OverrideRecord} from './catalog-types';
const object=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
const exact=(value:unknown,keys:string[]):value is Record<string,unknown>=>object(value)&&Object.keys(value).length===keys.length&&keys.every(key=>Object.hasOwn(value,key));
const sha=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{64}$/.test(value);
export function readLegacyRunAccepted(value:unknown):RunAccepted{
 if(!exact(value,['run_id','status'])||!scopeUUID(value.run_id)||value.status!=='queued')throw new Error('EDITOR_RUN_RECEIPT_INVALID');
 return {run_id:value.run_id,status:'queued'};
}
export function readLegacyMaskAccepted(value:unknown,sourceSHA:string):RunAccepted&{source_sha256:string}{
 if(!sha(sourceSHA)||!exact(value,['run_id','status','source_sha256'])||value.source_sha256!==sourceSHA)throw new Error('EDITOR_MASK_RECEIPT_INVALID');
 return {...readLegacyRunAccepted({run_id:value.run_id,status:value.status}),source_sha256:sourceSHA};
}
export function readGenerationAbandonment(value:unknown,key:string):RunAccepted|{status:'abandoned';key:string}{
 if(object(value)&&value.status==='abandoned'){
  if(!exact(value,['status','key'])||value.key!==key||!key)throw new Error('GENERATION_ABANDONMENT_INVALID');
  return {status:'abandoned',key};
 }
 return readLegacyRunAccepted(value);
}
export function readLegacyOverride(value:unknown,version:string,sourceSHA:string,expectedParams?:Record<string,unknown>):OverrideRecord{
 if(!exact(value,['id','asset_version_id','input_sha256','revision','config','config_hash','created_at'])||!scopeUUID(value.id)||value.asset_version_id!==version||!scopeUUID(version)||!sha(sourceSHA)||value.input_sha256!==sourceSHA||typeof value.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n||!object(value.config)||!sha(value.config_hash)||typeof value.created_at!=='string'||!/^\d{4}-\d\d-\d\dT/.test(value.created_at)||!Number.isFinite(Date.parse(value.created_at)))throw new Error('EDITOR_OVERRIDE_RECEIPT_INVALID');
 const config=value.config;
 if(config.schema_version!==1||config.input_sha256!==sourceSHA||!Number.isSafeInteger(config.revision)||BigInt(config.revision as number)!==BigInt(value.revision)||!object(config.modules)||bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(config))))!==value.config_hash)throw new Error('EDITOR_OVERRIDE_RECEIPT_INVALID');
 if(expectedParams&&canonicalJSON({...config,revision:1})!==canonicalJSON({...expectedParams,revision:1}))throw new Error('EDITOR_OVERRIDE_PARAMS_CHANGED');
 return structuredClone(value) as OverrideRecord;
}
