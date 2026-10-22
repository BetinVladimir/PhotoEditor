import {scopeUUID} from './workspace-access';
export type ApplyCapabilities={schema_version:1;tenant_id:string;project_id:string;collection_id:string;source_revision:string;style_version_id:string;style_sha256:string;available:boolean;reason:string|null;backends:Array<'cuda'|'mps'|'cpu'>;expires_at:string};
export function readApplyCapabilities(value:unknown,scope:{tenant:string;project:string;collection:string;revision:string;version:string;styleSHA?:string},now=Date.now()):ApplyCapabilities{
 const fail=():never=>{throw new Error('APPLY_CAPABILITY_INVALID')};
 if(!value||typeof value!=='object'||Array.isArray(value))return fail();const v=value as Record<string,unknown>;
 const keys=['schema_version','tenant_id','project_id','collection_id','source_revision','style_version_id','style_sha256','available','reason','backends','expires_at'];
 if(Object.keys(v).length!==keys.length||!keys.every(key=>Object.hasOwn(v,key))||v.schema_version!==1||![scope.tenant,scope.project,scope.collection,scope.version].every(scopeUUID)||v.tenant_id!==scope.tenant||v.project_id!==scope.project||v.collection_id!==scope.collection||v.source_revision!==scope.revision||v.style_version_id!==scope.version||typeof v.style_sha256!=='string'||!/^[0-9a-f]{64}$/.test(v.style_sha256)||scope.styleSHA!==undefined&&v.style_sha256!==scope.styleSHA||typeof v.available!=='boolean'||!Array.isArray(v.backends)||v.backends.length>3||v.backends.some(b=>!['cuda','mps','cpu'].includes(b))||new Set(v.backends).size!==v.backends.length||typeof v.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(v.expires_at)||!Number.isFinite(Date.parse(v.expires_at))||Date.parse(v.expires_at)<=now||Date.parse(v.expires_at)>now+20000)return fail();
 const reasons=['STYLE_MODEL_BINDING_MISSING','PERSON_MASK_CAPABILITY_UNAVAILABLE','RESTORATION_CAPABILITY_UNAVAILABLE','CREATIVE_CAPABILITY_UNAVAILABLE','VALIDATION_FAILED','OVERRIDE_MASK_INVALID','REGION_MASK_REQUIRED','REGION_MASK_LIMIT_EXCEEDED'];
 if(v.available?v.reason!==null:typeof v.reason!=='string'||!reasons.includes(v.reason))return fail();
 return structuredClone(v) as ApplyCapabilities;
}
