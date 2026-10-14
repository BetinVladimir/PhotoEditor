import {v2ResourceData} from './v2-resource';
export type InterchangeJob={id:string;tenant_id:string;project_id:string;workflow_id:string;revision:string;direction:'import'|'export';format:'xmp'|'photoeditor_bundle'|'captureone_sidecar';mapping_artifact_id:string|null;report_artifact_id:string|null;bundle_artifact_id:string|null};
export type InterchangeReviewTarget={entry_id:string;external_id:string;asset_version_id:string;state:'proposed';fields:{name:string;status:'exact'|'unsupported'|'rejected';reason:string|null}[]};
export type InterchangeReviewSummary={report:{artifact_id:string;sha256:string};proposed_count:number;entry_count:number;can_close:boolean;can_decide?:boolean;targets?:InterchangeReviewTarget[]};
export type InterchangeInspection={schema_version:1;workflow_id:string;tenant_id:string;project_id:string;run_id:string;revision:string;status:string;stage:string;ingest_run_id:string|null;collection_id:string|null;review?:InterchangeReviewSummary|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,15}$/.test(v)&&BigInt(v)<=9007199254740991n;
const nullableID=(v:unknown)=>v===null||uuid(v);
const exactKeys=(v:Record<string,unknown>,keys:readonly string[])=>Object.keys(v).length===keys.length&&keys.every(key=>Object.prototype.hasOwnProperty.call(v,key));
const timestamp=(v:unknown):v is string=>{
 if(typeof v!=='string')return false;
 const match=/^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(Z|[+-]\d\d:\d\d)$/.exec(v);
 if(!match)return false;
 const year=Number(match[1]),month=Number(match[2]),day=Number(match[3]);
 if(year<1||month<1||month>12||day<1||day>new Date(Date.UTC(year,month,0)).getUTCDate()||Number(match[4])>23||Number(match[5])>59||Number(match[6])>59)return false;
 if(match[7]!=='Z'&&(Number(match[7].slice(1,3))>23||Number(match[7].slice(4))>59))return false;
 return Number.isFinite(Date.parse(v));
};
const documentRef=(v:unknown)=>v===null||!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===2&&uuid((v as Record<string,unknown>).artifact_id)&&typeof (v as Record<string,unknown>).sha256==='string'&&/^[0-9a-f]{64}$/.test((v as Record<string,unknown>).sha256 as string);
export function assertInterchangeScope(tenant:string,project:string,id:string){if(!uuid(tenant)||!uuid(project)||!uuid(id))throw new Error('INTERCHANGE_SCOPE_INVALID');}
export function readInterchangeJob(value:unknown,tenant:string,project:string,id:string):InterchangeJob{
 assertInterchangeScope(tenant,project,id);
 const d=v2ResourceData<Record<string,unknown>>(value,'interchange_jobs',id);
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==4||Object.keys(d).length!==13||!documentRef((value as Record<string,unknown>).document)||d.tenant_id!==tenant||d.project_id!==project||d.workflow_id!==id||!timestamp(d.created_at)||!timestamp(d.updated_at)||Date.parse(d.updated_at)<Date.parse(d.created_at)||!revision(d.revision)||!['import','export'].includes(d.direction as string)||!['xmp','photoeditor_bundle','captureone_sidecar'].includes(d.format as string)||!nullableID(d.mapping_artifact_id)||!nullableID(d.report_artifact_id)||!nullableID(d.bundle_artifact_id))throw new Error('INTERCHANGE_JOB_INVALID');
 return {id,tenant_id:tenant,project_id:project,workflow_id:id,revision:d.revision,direction:d.direction as InterchangeJob['direction'],format:d.format as InterchangeJob['format'],mapping_artifact_id:d.mapping_artifact_id as string|null,report_artifact_id:d.report_artifact_id as string|null,bundle_artifact_id:d.bundle_artifact_id as string|null};
}
export function readInterchangeInspection(value:unknown,tenant:string,project:string,id:string):InterchangeInspection{
 assertInterchangeScope(tenant,project,id);
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('INTERCHANGE_INSPECTION_INVALID');
 const d=value as Record<string,unknown>;
 if(!exactKeys(d,['schema_version','workflow_id','tenant_id','project_id','run_id','revision','status','stage','ingest_run_id','collection_id','review'])||d.schema_version!==1||d.workflow_id!==id||d.tenant_id!==tenant||d.project_id!==project||!uuid(d.run_id)||!revision(d.revision)||!['queued','running','cancelling','waiting_review','completed','completed_with_errors','failed','cancelled'].includes(d.status as string)||!['queued','parsing','ingesting','preparing_import','exporting','finalizing','review','completed','failed','cancelled','cancelling'].includes(d.stage as string)||!nullableID(d.ingest_run_id)||!nullableID(d.collection_id))throw new Error('INTERCHANGE_INSPECTION_INVALID');
 let review:InterchangeReviewSummary|null=null;
 if(d.review!==null){
  if(typeof d.review!=='object'||Array.isArray(d.review))throw new Error('INTERCHANGE_REVIEW_INVALID');
  const v=d.review as Record<string,unknown>,pin=v.report as Record<string,unknown>;
  if(!exactKeys(v,['report','proposed_count','entry_count','can_close','can_decide','targets'])||!pin||typeof pin!=='object'||Array.isArray(pin)||!exactKeys(pin,['artifact_id','sha256'])||!uuid(pin.artifact_id)||typeof pin.sha256!=='string'||!/^[0-9a-f]{64}$/.test(pin.sha256)||!Number.isInteger(v.proposed_count)||!Number.isInteger(v.entry_count)||(v.entry_count as number)<1||(v.entry_count as number)>5000||(v.proposed_count as number)<0||(v.proposed_count as number)>(v.entry_count as number)||typeof v.can_close!=='boolean'||v.can_close&&(d.status!=='waiting_review'||v.proposed_count!==0))throw new Error('INTERCHANGE_REVIEW_INVALID');
  const targets:InterchangeReviewTarget[]=[];
  if(typeof v.can_decide!=='boolean'||v.can_decide&&d.status!=='waiting_review')throw new Error('INTERCHANGE_REVIEW_INVALID');
  if(!Array.isArray(v.targets)||v.targets.length>100||v.targets.length>(v.proposed_count as number))throw new Error('INTERCHANGE_REVIEW_TARGETS_INVALID');
   const seen=new Set<string>();
   for(const target of v.targets){
    if(!target||typeof target!=='object'||Array.isArray(target)||!exactKeys(target,['entry_id','external_id','asset_version_id','state','fields'])||!uuid(target.entry_id)||seen.has(target.entry_id)||!uuid(target.asset_version_id)||target.state!=='proposed'||typeof target.external_id!=='string'||target.external_id.length<1||target.external_id.length>240||!Array.isArray(target.fields)||target.fields.length>128)throw new Error('INTERCHANGE_REVIEW_TARGETS_INVALID');
    seen.add(target.entry_id);const fields:InterchangeReviewTarget['fields']=[],names=new Set<string>();
    for(const field of target.fields){
     if(!field||typeof field!=='object'||Array.isArray(field)||!exactKeys(field,['name','status','reason'])||typeof field.name!=='string'||field.name.length<1||field.name.length>80||names.has(field.name)||!['exact','unsupported','rejected'].includes(field.status)||(field.status==='exact'?field.reason!==null:typeof field.reason!=='string'||field.reason.length<1||field.reason.length>200))throw new Error('INTERCHANGE_REVIEW_TARGETS_INVALID');
     names.add(field.name);fields.push({name:field.name,status:field.status,reason:field.reason});
    }
    targets.push({entry_id:target.entry_id,external_id:target.external_id,asset_version_id:target.asset_version_id,state:'proposed',fields});
   }
  review={report:{artifact_id:pin.artifact_id,sha256:pin.sha256},proposed_count:v.proposed_count as number,entry_count:v.entry_count as number,can_close:v.can_close,can_decide:v.can_decide===true,targets};
 }
 const expected=d.status==='running'?null:d.status==='waiting_review'?'review':d.status==='completed_with_errors'?'completed':d.status;
 if(expected!==null&&d.stage!==expected||d.status==='running'&&!['parsing','ingesting','preparing_import','exporting','finalizing'].includes(d.stage as string))throw new Error('INTERCHANGE_INSPECTION_STATE_INVALID');
 if(['exporting','finalizing'].includes(d.stage as string)&&(d.ingest_run_id!==null||d.collection_id!==null||review!==null))throw new Error('INTERCHANGE_INSPECTION_STATE_INVALID');
 return {schema_version:1,workflow_id:id,tenant_id:tenant,project_id:project,run_id:d.run_id,revision:d.revision,status:d.status as string,stage:d.stage as string,ingest_run_id:d.ingest_run_id as string|null,collection_id:d.collection_id as string|null,review};
}

export function interchangeCloseRequest(inspection:InterchangeInspection,key:string){
 if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');
 if(inspection.status!=='waiting_review'||!inspection.review?.can_close||inspection.review.proposed_count!==0||!revision(inspection.revision)||BigInt(inspection.revision)>=9007199254740991n)throw new Error('INTERCHANGE_REVIEW_NOT_CLOSABLE');
 return {expected_revision:inspection.revision,report_sha:inspection.review.report.sha256};
}
export function readInterchangeCloseResult(value:unknown,inspection:InterchangeInspection){
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('INTERCHANGE_CLOSE_RECEIPT_INVALID');
 const v=value as Record<string,unknown>;
 if(v.run_id!==inspection.run_id||v.workflow_id!==inspection.workflow_id||v.state!=='completed'||v.revision!==(BigInt(inspection.revision)+1n).toString()||!Array.isArray(v.outputs)||v.outputs.length!==0||v.error_code!==null)throw new Error('INTERCHANGE_CLOSE_RECEIPT_INVALID');
 return {run_id:inspection.run_id,workflow_id:inspection.workflow_id,revision:v.revision as string,state:'completed' as const};
}

export function interchangeDecisionRequest(inspection:InterchangeInspection,decisions:{entry_id:string;decision:'approve'|'reject'}[],key:string){
 if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');
 if(inspection.status!=='waiting_review'||!inspection.review?.can_decide||decisions.length<1||decisions.length>100||BigInt(inspection.revision)>=9007199254740991n)throw new Error('INTERCHANGE_DECISIONS_INVALID');
 const available=new Set(inspection.review.targets?.map(t=>t.entry_id)),seen=new Set<string>();
 const owned=decisions.map(d=>{if(!available.has(d.entry_id)||seen.has(d.entry_id)||!['approve','reject'].includes(d.decision))throw new Error('INTERCHANGE_DECISIONS_INVALID');seen.add(d.entry_id);return {entry_id:d.entry_id,decision:d.decision};});
 return {expected_revision:inspection.revision,report_sha:inspection.review.report.sha256,decisions:owned};
}
export function readInterchangeDecisionResult(value:unknown,inspection:InterchangeInspection,decidedCount:number){
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('INTERCHANGE_DECISION_RECEIPT_INVALID');
 const v=value as Record<string,unknown>;
 if(v.workflow_id!==inspection.workflow_id||v.revision!==(BigInt(inspection.revision)+1n).toString()||!uuid(v.batch_id)||v.child!==null||v.remaining!==inspection.review!.proposed_count-decidedCount)throw new Error('INTERCHANGE_DECISION_RECEIPT_INVALID');
 return {workflow_id:inspection.workflow_id,revision:v.revision as string,batch_id:v.batch_id,remaining:v.remaining as number,child:null};
}

export function assertInterchangeSnapshot(job:InterchangeJob,inspection:InterchangeInspection):void{
 if(job.workflow_id!==inspection.workflow_id||job.tenant_id!==inspection.tenant_id||job.project_id!==inspection.project_id||job.revision!==inspection.revision)throw new Error('INTERCHANGE_SNAPSHOT_INVALID');
 const exportStage=['exporting','finalizing'].includes(inspection.stage);
 if(job.direction==='import'){
  if(exportStage)throw new Error('INTERCHANGE_SNAPSHOT_INVALID');
  return;
 }
 if(inspection.ingest_run_id!==null||inspection.collection_id!==null||inspection.review!=null||['parsing','ingesting','preparing_import','review'].includes(inspection.stage)||inspection.status==='waiting_review')throw new Error('INTERCHANGE_SNAPSHOT_INVALID');
 if((job.report_artifact_id===null)!==(job.bundle_artifact_id===null))throw new Error('INTERCHANGE_SNAPSHOT_INVALID');
 if(inspection.stage==='finalizing'&&job.bundle_artifact_id===null||inspection.stage==='exporting'&&job.bundle_artifact_id!==null)throw new Error('INTERCHANGE_SNAPSHOT_INVALID');
}
