import {lensProposalScope,readLensProposal,type LensProposal} from './lens-proposal';
import {readPerspectivePreview} from './perspective-preview';
import type {V2VersionBinding} from './catalog-types';
import type {ApiClient} from '@photoeditor/api-client';
import type {ArtifactRef} from './catalog-types';
import {multiUUID} from './multi-intent';
export type LensChoice={asset_version_id:string;profile:ArtifactRef;strength:number};
export type LensIntent={schema_version:1;snapshot_id:string;choices:LensChoice[]};
export type LensProfile={profile:ArtifactRef;camera_model:string;lens_model:string;focal_mm:number;aperture:number};
export type LensTask={ordinal:number;asset_version_id:string;task_id:string;state:string;result_id:string|null;result:ArtifactRef|null};
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&multiUUID(v.artifact_id)&&typeof v.sha256==='string'&&/^[0-9a-f]{64}$/.test(v.sha256);
export function freezeLensIntent(value:unknown,versions:readonly string[]):LensIntent{
 if(!fields(value,['schema_version','snapshot_id','choices'])||value.schema_version!==1||!multiUUID(value.snapshot_id)||!Array.isArray(value.choices)||value.choices.length<1||value.choices.length>5000||versions.length!==value.choices.length||new Set(versions).size!==versions.length||!versions.every(multiUUID))throw new Error('LENS_INTENT_INVALID');
 const expected=new Set(versions),seen=new Set<string>();
 for(const choice of value.choices){
  if(!fields(choice,['asset_version_id','profile','strength'])||!multiUUID(choice.asset_version_id)||!expected.has(choice.asset_version_id)||seen.has(choice.asset_version_id)||!ref(choice.profile)||typeof choice.strength!=='number'||!Number.isFinite(choice.strength)||choice.strength<0||choice.strength>1)throw new Error('LENS_INTENT_INVALID');
  seen.add(choice.asset_version_id);
 }
 return structuredClone(value) as LensIntent;
}
export function readLensProfiles(value:unknown,after?:string):{items:LensProfile[];next?:string}{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('LENS_CATALOG_INVALID');
 const page=value as Record<string,unknown>;
 if(Object.keys(page).some(k=>k!=='items'&&k!=='next')||!Array.isArray(page.items)||page.items.length>100||(page.next!==undefined&&!multiUUID(page.next)))throw new Error('LENS_CATALOG_INVALID');
 let last=after??'';
 for(const item of page.items){
  if(!fields(item,['profile','camera_model','lens_model','focal_mm','aperture'])||!ref(item.profile)||item.profile.artifact_id<=last||typeof item.camera_model!=='string'||item.camera_model.length<1||typeof item.lens_model!=='string'||item.lens_model.length<1||typeof item.focal_mm!=='number'||!Number.isFinite(item.focal_mm)||item.focal_mm<1||item.focal_mm>2000||typeof item.aperture!=='number'||!Number.isFinite(item.aperture)||item.aperture<.5||item.aperture>128)throw new Error('LENS_CATALOG_INVALID');
  last=item.profile.artifact_id;
 }
 if(page.next!==undefined&&(page.items.length!==100||page.next!==last))throw new Error('LENS_CATALOG_INVALID');
 return structuredClone(page) as {items:LensProfile[];next?:string};
}
export function readLensTasks(value:unknown,run:string,after=-1):{run_id:string;items:LensTask[];next?:number}{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('LENS_STATUS_INVALID');
 const page=value as Record<string,unknown>;
 if(Object.keys(page).some(k=>!['run_id','items','next'].includes(k))||page.run_id!==run||!multiUUID(run)||!Array.isArray(page.items)||page.items.length>100)throw new Error('LENS_STATUS_INVALID');
 let last=after;const tasks=new Set<string>(),versions=new Set<string>();
 for(const item of page.items){
  if(!fields(item,['ordinal','asset_version_id','task_id','state','result_id','result'])||typeof item.ordinal!=='number'||!Number.isInteger(item.ordinal)||item.ordinal<=last||item.ordinal>4999||!multiUUID(item.asset_version_id)||!multiUUID(item.task_id)||tasks.has(item.task_id)||versions.has(item.asset_version_id)||typeof item.state!=='string'||!['blocked','ready','queued','running','succeeded','failed','cancelled','skipped','retry_wait'].includes(item.state)||((item.result_id===null)!==(item.result===null))||(item.result!==null&&(!multiUUID(item.result_id)||!ref(item.result)||item.state!=='succeeded')))throw new Error('LENS_STATUS_INVALID');
  last=item.ordinal;tasks.add(item.task_id);versions.add(item.asset_version_id);
 }
 if(page.next!==undefined&&(page.items.length!==100||page.next!==last))throw new Error('LENS_STATUS_INVALID');
 return structuredClone(page) as {run_id:string;items:LensTask[];next?:number};
}
export class LensClient {
 private root:string;
 constructor(private client:ApiClient,private tenant:string,private project:string){if(!multiUUID(tenant)||!multiUUID(project))throw new Error('LENS_SCOPE_INVALID');this.root=`/api/v1/tenants/${tenant}/projects/${project}`;}
 async profiles(after?:string,signal?:AbortSignal){if(after!==undefined&&!multiUUID(after))throw new Error('LENS_CURSOR_INVALID');signal?.throwIfAborted();const raw=await this.client.request(`${this.root}/lens-profiles${after?`?after=${after}`:''}`,{signal});signal?.throwIfAborted();return readLensProfiles(raw,after);}
 async submit(intent:LensIntent,versions:readonly string[],key:string,signal?:AbortSignal){const frozen=freezeLensIntent(intent,versions);if(!/^[A-Za-z0-9._:-]{1,128}$/.test(key))throw new Error('LENS_KEY_INVALID');signal?.throwIfAborted();const raw=await this.client.request<unknown>(`${this.root}/lens-intents`,{method:'POST',body:frozen,idempotencyKey:key,signal});if(!fields(raw,['run_id','workflow_id','state'])||!multiUUID(raw.run_id)||!multiUUID(raw.workflow_id)||raw.state!=='queued')throw new Error('LENS_TICKET_INVALID');signal?.throwIfAborted();return structuredClone(raw) as {run_id:string;workflow_id:string;state:'queued'};}
 async status(run:string,after=-1,signal?:AbortSignal){if(!multiUUID(run)||!Number.isInteger(after)||after< -1||after>4999)throw new Error('LENS_STATUS_INVALID');signal?.throwIfAborted();const raw=await this.client.request(`${this.root}/lens-intents/${run}?after=${after}`,{signal});signal?.throwIfAborted();return readLensTasks(raw,run,after);}
 private proposalPath(base:V2VersionBinding,producer:string){if(!multiUUID(base.asset_version_id)||!multiUUID(producer))throw new Error('LENS_SCOPE_INVALID');return `${this.root}/versions/${base.asset_version_id}/lens-proposals/${producer}`;}
 async proposal(tenant:string,project:string,base:V2VersionBinding,producer:string,signal?:AbortSignal){if(tenant!==this.tenant||project!==this.project)throw new Error('LENS_SCOPE_INVALID');const scope=lensProposalScope(tenant,project,producer,base);signal?.throwIfAborted();const raw=await this.client.request(this.proposalPath(scope.base,scope.producer),{signal});signal?.throwIfAborted();return readLensProposal(raw,scope.tenant,scope.project,scope.producer,scope.base);}
 async preview(proposal:LensProposal,signal?:AbortSignal){proposal=readLensProposal(proposal,this.tenant,this.project,proposal.producer_task_id,proposal.base);signal?.throwIfAborted();const raw=await this.client.request(`${this.proposalPath(proposal.base,proposal.producer_task_id)}/preview?variant=preview1600`,{signal});signal?.throwIfAborted();return readPerspectivePreview(raw,proposal,'preview1600');}
 async apply(proposal:LensProposal,acceptUncovered:boolean,signal?:AbortSignal){
  proposal=readLensProposal(proposal,this.tenant,this.project,proposal.producer_task_id,proposal.base);
  if(proposal.requires_review&&!acceptUncovered)throw new Error('LENS_ACKNOWLEDGEMENT_REQUIRED');
  const body={schema_version:1,result_id:proposal.result_id,root:structuredClone(proposal.result),working_image:structuredClone(proposal.working_image),confirm_geometry_reset:true,accept_uncovered_area:acceptUncovered};
  signal?.throwIfAborted();
  const value=await this.client.request<unknown>(`${this.proposalPath(proposal.base,proposal.producer_task_id)}/apply`,{method:'POST',body,signal});
  signal?.throwIfAborted();
  if(!fields(value,['asset_version_id','parent_asset_version_id','source'])||!multiUUID(value.asset_version_id)||value.parent_asset_version_id!==proposal.base.asset_version_id||!ref(value.source)||value.source.artifact_id!==proposal.working_image.artifact_id||value.source.sha256!==proposal.working_image.sha256)throw new Error('LENS_APPLIED_INVALID');
  return structuredClone(value) as {asset_version_id:string;parent_asset_version_id:string;source:ArtifactRef};
 }

}
