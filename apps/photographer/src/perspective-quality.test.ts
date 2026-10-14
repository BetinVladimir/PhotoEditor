import {preparePerspectiveApply,readPerspectiveApplied} from './perspective-apply';
import {describe,it,expect} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {PerspectiveProposal} from './perspective-proposal';
import {perspectiveQualityTask,readPerspectiveQuality,type PerspectiveQuality} from './perspective-quality';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:PerspectiveProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};

function seal(v:PerspectiveQuality){v.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v.quality))));}
function fixture():PerspectiveQuality{const v:PerspectiveQuality={proposal:structuredClone(proposal),quality_run_id:id(20),quality_task_id:perspectiveQualityTask(id(20)),quality_result_id:id(22),quality_report:ref(23),quality:{target:ref(8),target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};seal(v);return v;}
describe('perspective technical quality',()=>{
 it('owns data and retains review/fail decisions',()=>{
  for(const [black,status] of [[0,'pass'],[.03,'review'],[.11,'fail']] as const){const v=fixture();v.quality.technical.clipped_black_fraction=black;v.quality.status=status;v.quality.reasons=black?['excessive_black_clipping']:[];seal(v);const owned=readPerspectiveQuality(v,proposal);v.quality.reasons.push('changed');expect(owned.quality.status).toBe(status);expect(owned.quality.reasons).not.toContain('changed');}
 });
 it('rejects stale target, fake pass, nonfinite metrics and aliases',()=>{
  for(const mutate of [(v:PerspectiveQuality)=>{v.quality.target=ref(24);},(v:PerspectiveQuality)=>{v.quality.technical.clipped_black_fraction=.2;},(v:PerspectiveQuality)=>{v.quality.technical.sharpness_laplacian=NaN;},(v:PerspectiveQuality)=>{v.proposal.result_id=id(25);},(v:PerspectiveQuality)=>{v.proposal.changed_mask=ref(25);},(v:PerspectiveQuality)=>{v.proposal.report=ref(26);},(v:PerspectiveQuality)=>{v.proposal.canvas.width=101;},(v:PerspectiveQuality)=>{v.quality_report=ref(8);},(v:PerspectiveQuality)=>{v.quality.reasons=['low_sharpness'];}]){const v=fixture();mutate(v);expect(()=>readPerspectiveQuality(v,proposal)).toThrow();}
 });
 it('refuses a quality payload changed after its report digest was pinned',()=>{
  const v=fixture();v.quality.technical.sharpness_laplacian=.02;
  expect(()=>readPerspectiveQuality(v,proposal)).toThrow('PERSPECTIVE_QUALITY_INVALID');
 });
 it('requires the quality task derived from the exact quality run',()=>{
  const v=fixture();expect(perspectiveQualityTask(v.quality_run_id)).toMatch(/^[0-9a-f-]{36}$/);
  v.quality_task_id=id(21);expect(()=>readPerspectiveQuality(v,proposal)).toThrow('PERSPECTIVE_QUALITY_INVALID');
  v.quality_task_id=perspectiveQualityTask(id(24));expect(()=>readPerspectiveQuality(v,proposal)).toThrow('PERSPECTIVE_QUALITY_INVALID');
 });
});

describe('perspective apply decision',()=>{
 it('requires geometry and warning confirmation and refuses fail',()=>{
  const v=fixture();expect(()=>preparePerspectiveApply(v,false,false)).toThrow();
  const decision=preparePerspectiveApply(v,true,false);v.proposal.result.sha256='b'.repeat(64);expect(decision.root.sha256).toBe('a'.repeat(64));
  const review=fixture();review.quality.technical.clipped_black_fraction=.03;review.quality.status='review';review.quality.reasons=['excessive_black_clipping'];
  expect(()=>preparePerspectiveApply(review,true,false)).toThrow();expect(preparePerspectiveApply(review,true,true).accept_quality_warnings).toBe(true);
  review.quality.technical.clipped_black_fraction=.11;review.quality.status='fail';expect(()=>preparePerspectiveApply(review,true,true)).toThrow();
 });
 it('accepts only a new version of the expected parent and exact target',()=>{
  const v=fixture(),result={asset_version_id:id(30),parent_asset_version_id:v.proposal.base.asset_version_id,source:ref(8)};
  expect(readPerspectiveApplied(result,v).asset_version_id).toBe(id(30));
  for(const patch of [{asset_version_id:id(6)},{parent_asset_version_id:id(31)},{source:ref(32)},{extra:true}])expect(()=>readPerspectiveApplied({...result,...patch},v)).toThrow();
 });
});
