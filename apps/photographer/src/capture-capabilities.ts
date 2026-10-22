import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {scopeUUID} from './workspace-access';
import type {ShootSession} from './capture-types';
export type CaptureCapabilities={schema_version:1;tenant_id:string;project_id:string;template_id:string;template_revision:string;policy_sha:string;available:boolean;reason:string|null;expires_at:string};
export function capturePolicySHA(source:Pick<ShootSession,'policy'|'capture_policy'>){return bytesToHex(sha256(new TextEncoder().encode(canonicalJSON({policy:source.policy,capture_policy:source.capture_policy}))));}
export function readCaptureCapabilities(value:unknown,scope:{tenant:string;project:string;template:string;revision:string;policySHA:string},now=Date.now()):CaptureCapabilities{
 const fail=():never=>{throw new Error('CAPTURE_CAPABILITY_INVALID')};if(!value||typeof value!=='object'||Array.isArray(value))return fail();const v=value as Record<string,unknown>,keys=['schema_version','tenant_id','project_id','template_id','template_revision','policy_sha','available','reason','expires_at'];
 if(Object.keys(v).length!==keys.length||!keys.every(k=>Object.hasOwn(v,k))||v.schema_version!==1||![scope.tenant,scope.project,scope.template].every(scopeUUID)||v.tenant_id!==scope.tenant||v.project_id!==scope.project||v.template_id!==scope.template||v.template_revision!==scope.revision||v.policy_sha!==scope.policySHA||!/^[0-9a-f]{64}$/.test(scope.policySHA)||typeof v.available!=='boolean'||typeof v.expires_at!=='string'||!Number.isFinite(Date.parse(v.expires_at))||Date.parse(v.expires_at)<=now||Date.parse(v.expires_at)>now+20000)return fail();
 if(v.available?v.reason!==null:typeof v.reason!=='string'||!['SESSION_UNAVAILABLE','CAPTURE_UNAVAILABLE','CALIBRATION_UNAVAILABLE'].includes(v.reason))return fail();return structuredClone(v) as CaptureCapabilities;
}
