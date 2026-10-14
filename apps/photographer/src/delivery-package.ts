import type {ArtifactRef} from './catalog-types';
import {v2ResourceData} from './v2-resource';
export type DeliveryPackage = {
 id:string;tenant_id:string;project_id:string;revision:string;
 workflow_id:string;snapshot_id:string;name:string;
 policy_artifact_id:string;manifest_artifact_id:string|null;
 published_at:string|null;state:'building'|'review'|'ready'|'failed'|'cancelled';
 document:ArtifactRef|null;
};
export type DeliveryPublishIntent={id:string;project_id:string;expected_revision:string};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export const deliveryRevision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]*$/.test(v)&&v.length<=16&&BigInt(v)<=9007199254740991n;
export const deliveryTimestamp=(v:unknown):v is string=>{
 if(typeof v!=='string')return false;
 const parts=/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.exec(v);
 if(!parts)return false;
 const [,year,month,day,hour,minute,second]=parts.map(Number);
 const leap=year%4===0&&(year%100!==0||year%400===0);
 const days=[31,leap?29:28,31,30,31,30,31,31,30,31,30,31];
 if(year<1||month<1||month>12||day<1||day>(days[month-1]??0)||hour>23||minute>59||second>59)return false;
 if(!v.endsWith('Z')){const zone=v.slice(-5);if(Number(zone.slice(0,2))>23||Number(zone.slice(3))>59)return false;}
 return Number.isFinite(Date.parse(v));
};
const invalid=():never=>{throw new Error('DELIVERY_PACKAGE_INVALID');};
export function assertDeliveryScope(tenant:string,project:string,id:string):void{
 if(!uuid(tenant)||!uuid(project)||!uuid(id))invalid();
}
/** Bind metadata to the current tenant/project before exposing a publish action. */
export function readDeliveryPackage(value:unknown,tenant:string,project:string,id:string):DeliveryPackage{
 assertDeliveryScope(tenant,project,id);
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==4||Object.keys(value).some(key=>!['id','revision','document','data'].includes(key)))return invalid();
 const data=v2ResourceData<Record<string,unknown>>(value,'delivery_packages',id);
 const allowed=new Set(['resource_type','id','tenant_id','project_id','created_at','revision','updated_at','workflow_id','snapshot_id','name','policy_artifact_id','manifest_artifact_id','published_at','state']);
 if(Object.keys(data).length!==allowed.size||Object.keys(data).some(key=>!allowed.has(key))||!deliveryTimestamp(data.created_at)||!deliveryTimestamp(data.updated_at)||Date.parse(data.updated_at)<Date.parse(data.created_at))return invalid();
 const envelope=value as {document?:unknown};
 if(data.tenant_id!==tenant||data.project_id!==project||!deliveryRevision(data.revision)||!uuid(data.workflow_id)||!uuid(data.snapshot_id)||!uuid(data.policy_artifact_id)||typeof data.name!=='string'||Array.from(data.name).length<1||Array.from(data.name).length>200||!['building','review','ready','failed','cancelled'].includes(data.state as string))return invalid();
 if(data.manifest_artifact_id!==null&&!uuid(data.manifest_artifact_id))return invalid();
 if(data.published_at!==null&&!deliveryTimestamp(data.published_at))return invalid();
 let document:ArtifactRef|null=null;
 if(envelope.document!==null){
  const ref=envelope.document as Record<string,unknown>|undefined;
  if(!ref||typeof ref!=='object'||Array.isArray(ref)||Object.keys(ref).length!==2||!uuid(ref.artifact_id)||typeof ref.sha256!=='string'||!/^[0-9a-f]{64}$/.test(ref.sha256)||ref.artifact_id!==data.manifest_artifact_id)return invalid();
  document={artifact_id:ref.artifact_id,sha256:ref.sha256};
 }
 if(['review','ready'].includes(data.state as string)&&data.manifest_artifact_id===null)return invalid();
 if(data.published_at!==null&&(data.state!=='ready'||data.manifest_artifact_id===null))return invalid();
 return {id,tenant_id:tenant,project_id:project,revision:data.revision,workflow_id:data.workflow_id,snapshot_id:data.snapshot_id,name:data.name,policy_artifact_id:data.policy_artifact_id,manifest_artifact_id:data.manifest_artifact_id as string|null,published_at:data.published_at as string|null,state:data.state as DeliveryPackage['state'],document};
}
/** Keep the original revision/key for retry; a refresh must not rewrite the intent. */
export function freezeDeliveryPublish(intent:DeliveryPublishIntent,key:string):DeliveryPublishIntent{
 if(!intent||Object.keys(intent).length!==3||!uuid(intent.id)||!uuid(intent.project_id)||!deliveryRevision(intent.expected_revision)||BigInt(intent.expected_revision)>=9007199254740991n||typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key))throw new Error('DELIVERY_PUBLISH_INVALID');
 return {id:intent.id,project_id:intent.project_id,expected_revision:intent.expected_revision};
}
export function readPublishedDelivery(value:unknown,tenant:string,intent:DeliveryPublishIntent):DeliveryPackage{
 const result=readDeliveryPackage(value,tenant,intent.project_id,intent.id);
 if(result.state!=='ready'||result.published_at===null||result.document===null||BigInt(result.revision)!==BigInt(intent.expected_revision)+1n)throw new Error('DELIVERY_PUBLICATION_MISMATCH');
 return result;
}
/** The review action must explicitly accept the displayed frozen manifest. */
export function prepareDeliveryPublication(pkg:DeliveryPackage,acceptReview:boolean):DeliveryPublishIntent{
 if(typeof acceptReview!=='boolean'||!['review','ready'].includes(pkg.state)||pkg.published_at!==null||pkg.document===null||pkg.manifest_artifact_id!==pkg.document.artifact_id)throw new Error('DELIVERY_NOT_PUBLISHABLE');
 if(pkg.state==='review'&&!acceptReview)throw new Error('DELIVERY_REVIEW_CONFIRMATION_REQUIRED');
 return freezeDeliveryPublish({id:pkg.id,project_id:pkg.project_id,expected_revision:pkg.revision},'prepare');
}
