import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {sha1} from '@noble/hashes/legacy.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readPerspectiveProposal,type PerspectiveProposal} from './perspective-proposal';
import type {ArtifactRef} from './catalog-types';
type Technical={below_zero_fraction:number;above_one_fraction:number;clipped_black_fraction:number;clipped_white_fraction:number;sharpness_laplacian:number;seam_p99_delta:number;finite:boolean};
export type PerspectiveQuality={proposal:PerspectiveProposal;quality_run_id:string;quality_task_id:string;quality_result_id:string;quality_report:ArtifactRef;quality:{target:ArtifactRef;target_kind:'working';stage:'master';threshold_profile_id:'master_v1';status:'pass'|'review'|'fail';technical:Technical;reference:null;reasons:string[]}};
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&typeof v.sha256==='string'&&/^[0-9a-f]{64}$/.test(v.sha256);
/** Matches the server's uuid.NewSHA1(qualityRun, "quality.check:master"). */
export function perspectiveQualityTask(qualityRun:string):string{
 if(!uuid(qualityRun))throw new Error('PERSPECTIVE_QUALITY_RUN_INVALID');
 const raw=qualityRun.replaceAll('-',''),name=new TextEncoder().encode('quality.check:master'),bytes=new Uint8Array(16+name.length);
 for(let i=0;i<16;i++)bytes[i]=parseInt(raw.slice(i*2,i*2+2),16);
 bytes.set(name,16);const hash=sha1(bytes).slice(0,16);hash[6]=(hash[6]&15)|80;hash[8]=(hash[8]&63)|128;
 const hex=Array.from(hash,v=>v.toString(16).padStart(2,'0')).join('');
 return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
}
export function readPerspectiveQuality(value:unknown,expected:PerspectiveProposal):PerspectiveQuality{
 const fail=()=>{throw new Error('PERSPECTIVE_QUALITY_INVALID');};
 if(!fields(value,['proposal','quality_run_id','quality_task_id','quality_result_id','quality_report','quality']))fail();
 const v=value as PerspectiveQuality;
 const p=readPerspectiveProposal(v.proposal,expected.tenant_id,expected.project_id,expected.producer_task_id,expected.base);
 const sameRef=(a:ArtifactRef,b:ArtifactRef)=>a.artifact_id===b.artifact_id&&a.sha256===b.sha256;
 if(p.result_id!==expected.result_id||!sameRef(p.result,expected.result)||!sameRef(p.working_image,expected.working_image)||!sameRef(p.changed_mask,expected.changed_mask)||!sameRef(p.report,expected.report)||p.status!==expected.status||p.requires_review!==expected.requires_review||Object.keys(p.canvas).some(key=>p.canvas[key as keyof typeof p.canvas]!==expected.canvas[key as keyof typeof expected.canvas]))fail();
 if(!uuid(v.quality_run_id)||!uuid(v.quality_task_id)||!uuid(v.quality_result_id)||!ref(v.quality_report)||new Set([v.quality_run_id,v.quality_task_id,v.quality_result_id]).size!==3||v.quality_task_id!==perspectiveQualityTask(v.quality_run_id))fail();
 if([p.result,p.working_image,p.changed_mask,p.report,p.base.source].some(r=>r.artifact_id===v.quality_report.artifact_id))fail();
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
