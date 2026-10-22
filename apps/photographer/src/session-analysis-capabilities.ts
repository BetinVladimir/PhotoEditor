import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {scopeUUID} from './workspace-access';
import type {SceneAnalysisRequest} from './scene-analysis-request';
export function sessionAnalysisRequestSHA(request:SceneAnalysisRequest){return bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(request))));}
export type SessionAnalysisCapabilities={schema_version:1;tenant_id:string;project_id:string;session_id:string;request_sha:string;model_lock_sha:string;expires_at:string};
export function readSessionAnalysisCapabilities(value:unknown,scope:{tenant:string;session:string;requestSHA:string},now=Date.now()):SessionAnalysisCapabilities{
 const fail=():never=>{throw new Error('SESSION_ANALYSIS_CAPABILITY_INVALID')};if(!value||typeof value!=='object'||Array.isArray(value))return fail();const v=value as Record<string,unknown>,keys=['schema_version','tenant_id','project_id','session_id','request_sha','model_lock_sha','expires_at'];
 if(Object.keys(v).length!==keys.length||!keys.every(k=>Object.hasOwn(v,k))||v.schema_version!==1||!scopeUUID(scope.tenant)||!scopeUUID(scope.session)||v.tenant_id!==scope.tenant||v.session_id!==scope.session||typeof v.project_id!=='string'||!scopeUUID(v.project_id)||v.request_sha!==scope.requestSHA||!/^[0-9a-f]{64}$/.test(scope.requestSHA)||typeof v.model_lock_sha!=='string'||!/^[0-9a-f]{64}$/.test(v.model_lock_sha)||typeof v.expires_at!=='string'||!Number.isFinite(Date.parse(v.expires_at))||Date.parse(v.expires_at)<=now||Date.parse(v.expires_at)>now+20000)return fail();
 return structuredClone(v) as SessionAnalysisCapabilities;
}
