import {scopeUUID} from './workspace-access';
export type AnalysisCapabilities={schema_version:1;tenant_id:string;project_id:string;style_id:string;geometry_enabled:boolean;available:boolean;reason:string|null;backends:Array<'cpu'|'cuda'|'mps'>;expires_at:string};
export function readAnalysisCapabilities(value:unknown,scope:{tenant:string;style:string;geometry:boolean},now=Date.now()):AnalysisCapabilities{
 const fail=():never=>{throw new Error('ANALYSIS_CAPABILITY_INVALID')};
 if(!value||typeof value!=='object'||Array.isArray(value))return fail();const v=value as Record<string,unknown>;
 const keys=['schema_version','tenant_id','project_id','style_id','geometry_enabled','available','reason','backends','expires_at'];
 if(Object.keys(v).length!==keys.length||!keys.every(key=>Object.hasOwn(v,key))||v.schema_version!==1||![scope.tenant,scope.style,v.project_id].every(scopeUUID)||v.tenant_id!==scope.tenant||v.style_id!==scope.style||v.geometry_enabled!==scope.geometry||typeof v.available!=='boolean'||!Array.isArray(v.backends)||v.backends.length>1||v.backends.some(b=>!['cpu','cuda','mps'].includes(b))||typeof v.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(v.expires_at)||!Number.isFinite(Date.parse(v.expires_at))||Date.parse(v.expires_at)<=now||Date.parse(v.expires_at)>now+20000)return fail();
 if(v.available?v.reason!==null||v.backends.length!==1:!['STYLE_FIT_CAPABILITY_UNAVAILABLE','STYLE_AGGREGATE_CAPABILITY_UNAVAILABLE'].includes(v.reason as string)||v.backends.length!==0)return fail();
 return structuredClone(v) as AnalysisCapabilities;
}
