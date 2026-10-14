import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readMultiProposal,type MultiProposal} from './multi-proposal';
import type {MultiIntent} from './multi-intent';
import type {ArtifactRef} from './catalog-types';
type Technical={below_zero_fraction:number;above_one_fraction:number;clipped_black_fraction:number;clipped_white_fraction:number;sharpness_laplacian:number;seam_p99_delta:number;finite:boolean};
export type MultiQuality={proposal:MultiProposal;quality_run_id:string;quality_task_id:string;quality_result_id:string;quality_report:ArtifactRef;quality:{target:ArtifactRef;target_kind:'working';stage:'master';threshold_profile_id:'master_v1';status:'pass'|'review'|'fail';technical:Technical;reference:null;reasons:string[]}};
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&typeof v.sha256==='string'&&/^[0-9a-f]{64}$/.test(v.sha256);
export function readMultiQuality(value:unknown,expected:MultiProposal,intent:MultiIntent):MultiQuality{
 const fail=()=>{throw new Error('MULTI_QUALITY_INVALID');};
 if(!fields(value,['proposal','quality_run_id','quality_task_id','quality_result_id','quality_report','quality']))fail();
 const v=value as MultiQuality;
 const owned=readMultiProposal(expected,expected.tenant_id,expected.project_id,expected.producer_task_id,expected.operation,intent);
 const p=readMultiProposal(v.proposal,owned.tenant_id,owned.project_id,owned.producer_task_id,owned.operation,intent);
 if(p.result_id!==expected.result_id||p.result.artifact_id!==expected.result.artifact_id||p.result.sha256!==expected.result.sha256||p.working_image.artifact_id!==expected.working_image.artifact_id||p.working_image.sha256!==expected.working_image.sha256)fail();
 const sameRef=(a:ArtifactRef,b:ArtifactRef)=>a.artifact_id===b.artifact_id&&a.sha256===b.sha256;
 if(!sameRef(p.changed_mask,owned.changed_mask)||!sameRef(p.report,owned.report)||p.reason!==owned.reason||p.registrations.some((r,i)=>!sameRef(r,owned.registrations[i]))||Object.keys(p.canvas).some(k=>p.canvas[k as keyof typeof p.canvas]!==owned.canvas[k as keyof typeof owned.canvas]))fail();
 if(!uuid(v.quality_run_id)||!uuid(v.quality_task_id)||!uuid(v.quality_result_id)||!ref(v.quality_report)||new Set([v.quality_run_id,v.quality_task_id,v.quality_result_id]).size!==3)fail();
 if([p.result,p.working_image,p.changed_mask,p.report,...p.sources.map(s=>s.source),...p.registrations,intent.params.calibration].some(r=>r.artifact_id===v.quality_report.artifact_id))fail();
 const q=v.quality;
 if(!fields(q,['target','target_kind','stage','threshold_profile_id','status','technical','reference','reasons'])||!ref(q.target)||q.target.artifact_id!==p.working_image.artifact_id||q.target.sha256!==p.working_image.sha256||q.target_kind!=='working'||q.stage!=='master'||q.threshold_profile_id!=='master_v1'||q.reference!==null)fail();
 const t=q.technical;
 if(!fields(t,['below_zero_fraction','above_one_fraction','clipped_black_fraction','clipped_white_fraction','sharpness_laplacian','seam_p99_delta','finite']))fail();
 for(const n of [t.below_zero_fraction,t.above_one_fraction,t.clipped_black_fraction,t.clipped_white_fraction,t.seam_p99_delta])if(typeof n!=='number'||!Number.isFinite(n)||n<0||n>1)fail();
 if(typeof t.finite!=='boolean'||typeof t.sharpness_laplacian!=='number'||!Number.isFinite(t.sharpness_laplacian)||t.sharpness_laplacian<0||t.sharpness_laplacian>1000000||t.seam_p99_delta!==0)fail();
 const reasons:string[]=[];let failure=false;
 if(!t.finite){reasons.push('nonfinite');failure=true;}
 if(t.clipped_black_fraction>.02){reasons.push('excessive_black_clipping');failure ||= t.clipped_black_fraction>.1;}
 if(t.clipped_white_fraction>.02){reasons.push('excessive_white_clipping');failure ||= t.clipped_white_fraction>.1;}
 if(t.sharpness_laplacian<1e-6)reasons.push('low_sharpness');
 if(q.status!==(failure?'fail':reasons.length?'review':'pass')||!Array.isArray(q.reasons)||q.reasons.length!==reasons.length||reasons.some((r,i)=>q.reasons[i]!==r))fail();
 const body=new TextEncoder().encode(canonicalJSON(q));
 if(body.byteLength>1048576||bytesToHex(sha256(body))!==v.quality_report.sha256)fail();
 return structuredClone(v);
}
