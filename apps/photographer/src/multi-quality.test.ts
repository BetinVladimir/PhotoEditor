import {perspectiveQualityTask} from './perspective-quality';
import {prepareMultiApply,readMultiApplied} from './multi-apply';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {describe,it,expect} from 'vitest';
import {readMultiQuality,type MultiQuality} from './multi-quality';
import type {MultiProposal} from './multi-proposal';
import type {MultiIntent,MultiOperation} from './multi-intent';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function fixture(op:MultiOperation){
 const frames=[{asset_version_id:id(20),source:ref(21),canvas_hash:'b'.repeat(64)},{asset_version_id:id(22),source:ref(23),canvas_hash:'c'.repeat(64)}];
 const common={schema_version:1 as const,frames,reference_ordinal:1,max_pixels:10000,calibration:ref(24)};
 const intent:MultiIntent={schema_version:1,snapshot_id:id(25),params:op==='optical.panorama'?{...common,projection:'planar'}:{...common,motion_policy:'review'}};
 const proposal:MultiProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),operation:op,result_id:id(4),result:ref(5),sources:structuredClone(frames),reference_ordinal:1,working_image:ref(6),changed_mask:ref(7),report:ref(8),registrations:[ref(9),ref(10)],canvas:{width:100,height:100,orientation:1,space_id:'multi:'+'d'.repeat(32),source_sha256:'e'.repeat(64)},status:'review',requires_review:true,reason:op.slice(8).toUpperCase()+'_REVIEW;motion_fraction=0.125;REGISTRATION_INCONSISTENT'};
 return {intent,proposal};
}

function quality(proposal:MultiProposal):MultiQuality{const value:MultiQuality={proposal:structuredClone(proposal),quality_run_id:id(40),quality_task_id:perspectiveQualityTask(id(40)),quality_result_id:id(42),quality_report:ref(43),quality:{target:proposal.working_image,target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};seal(value);return value;}
function seal(v:MultiQuality){v.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v.quality))));}
for(const op of ['optical.panorama','optical.focus_stack'] as const){
 it(op+' retains decisions and owns response',()=>{
  const {proposal,intent}=fixture(op);
  for(const [black,status] of [[0,'pass'],[.03,'review'],[.11,'fail']] as const){
   const v=quality(proposal);v.quality.technical.clipped_black_fraction=black;v.quality.status=status;v.quality.reasons=black?['excessive_black_clipping']:[];
   seal(v);const got=readMultiQuality(v,proposal,intent);v.quality.reasons.push('changed');expect(got.quality.status).toBe(status);expect(got.quality.reasons).not.toContain('changed');
  }
 });
 it(op+' refuses source aliases, stale geometry and fabricated pass',()=>{
  const {proposal,intent}=fixture(op);
  for(const change of [(v:MultiQuality)=>{v.quality_report=proposal.sources[0].source;},(v:MultiQuality)=>{v.quality.technical.clipped_black_fraction=.2;},(v:MultiQuality)=>{v.proposal.canvas.width=99;},(v:MultiQuality)=>{v.quality.target=ref(60);},(v:MultiQuality)=>{v.proposal.registrations[0]=ref(61);}]){
   const v=quality(proposal);change(v);expect(()=>readMultiQuality(v,proposal,intent)).toThrow();
  }
 });
 it(op+' freezes request scope before transport',async()=>{
  const {proposal,intent}=fixture(op),response=quality(proposal);let finish!:(v:unknown)=>void;let path='';
  const client={request:(p:string)=>{path=p;return new Promise(resolve=>{finish=resolve;});}} as unknown as ApiClient;
  const pending=new OwnerAPI(client,id(1)).multiQuality(proposal,intent);
  intent.params.frames.reverse();proposal.result.sha256='b'.repeat(64);finish(response);
  expect((await pending).proposal.result.sha256).toBe('a'.repeat(64));expect(path).toContain(op==='optical.panorama'?'/panorama-proposals/':'/focus-stack-proposals/');expect(path.endsWith('/quality')).toBe(true);
 });
}
it('cancelled quality read never reaches transport',async()=>{
 const {proposal,intent}=fixture('optical.panorama');let calls=0;
 const client={request:()=>{calls++;}} as unknown as ApiClient;const c=new AbortController();c.abort();
 await expect(new OwnerAPI(client,id(1)).multiQuality(proposal,intent,c.signal)).rejects.toThrow('MULTI_QUALITY_ABORTED');expect(calls).toBe(0);
});

it('rejects a changed report even when its altered metrics still produce pass',()=>{
 const {proposal,intent}=fixture('optical.panorama');const v=quality(proposal);
 v.quality.technical.sharpness_laplacian=.02;
 expect(()=>readMultiQuality(v,proposal,intent)).toThrow();
 seal(v);expect(readMultiQuality(v,proposal,intent).quality.technical.sharpness_laplacian).toBe(.02);
 v.quality_report.sha256='b'.repeat(64);expect(()=>readMultiQuality(v,proposal,intent)).toThrow();
});
it('hashes canonical report independent of transport field order',()=>{
 const {proposal,intent}=fixture('optical.focus_stack');const v=quality(proposal);
 v.quality=Object.fromEntries(Object.entries(v.quality).reverse()) as MultiQuality['quality'];
 expect(readMultiQuality(v,proposal,intent).quality_report.sha256).toBe(v.quality_report.sha256);
});

it('multi Apply requires explicit confirmations and exact new reference-parent version',()=>{
 const {proposal,intent}=fixture('optical.panorama'),v=quality(proposal);
 expect(()=>prepareMultiApply(v,false,false,intent)).toThrow();
 expect(prepareMultiApply(v,true,false,intent).root).toEqual(proposal.result);
 v.quality.status='review';v.quality.technical.clipped_black_fraction=.03;v.quality.reasons=['excessive_black_clipping'];seal(v);
 expect(()=>prepareMultiApply(v,true,false,intent)).toThrow();expect(prepareMultiApply(v,true,true,intent).accept_quality_warnings).toBe(true);
 v.quality.status='fail';v.quality.technical.clipped_black_fraction=.11;seal(v);expect(()=>prepareMultiApply(v,true,true,intent)).toThrow();
 const current=quality(proposal),result={asset_version_id:id(70),parent_asset_version_id:proposal.sources[1].asset_version_id,source:proposal.working_image};
 expect(readMultiApplied(result,current).asset_version_id).toBe(id(70));
 for(const patch of [{asset_version_id:proposal.sources[0].asset_version_id},{parent_asset_version_id:proposal.sources[0].asset_version_id},{source:ref(71)},{extra:true}])expect(()=>readMultiApplied({...result,...patch},current)).toThrow();
});

it('binds independent quality task identity to its actual admitted run',()=>{
 const {proposal,intent}=fixture('optical.panorama'),value=quality(proposal);
 expect(readMultiQuality(value,proposal,intent).quality_task_id).toBe(perspectiveQualityTask(value.quality_run_id));
 value.quality_task_id=id(99);expect(()=>readMultiQuality(value,proposal,intent)).toThrow('MULTI_QUALITY_INVALID');
});
