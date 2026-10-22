import {it,expect} from 'vitest';
import type {PerspectiveProposal} from './perspective-proposal';
import {perspectiveQualityTask,type PerspectiveQuality} from './perspective-quality';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {PerspectiveApplyAttempt} from './perspective-apply-attempt';
import {PerspectiveApplyStore,clearPerspectiveApplyAttempts} from './perspective-apply-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:PerspectiveProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};

function fixture():PerspectiveQuality{const v:PerspectiveQuality={proposal:structuredClone(proposal),quality_run_id:id(20),quality_task_id:perspectiveQualityTask(id(20)),quality_result_id:id(22),quality_report:ref(23),quality:{target:ref(8),target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};v.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v.quality))));return v;}

function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};}
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
it('owns journal scope and refuses substituted pending decisions',async()=>{
 const disk=storage(),store=new PerspectiveApplyStore(disk,'https://studio.test',id(50),id(1),id(2),id(6),lock);
 const attempt=await PerspectiveApplyAttempt.create(fixture(),false,null,store);
 expect((await store.read())?.review.quality_result_id).toBe(id(22));
 const bad=attempt.snapshot;bad.warnings=true;await expect(async()=>store.write(bad)).rejects.toThrow('CONFLICT');
 await expect(store.clear(id(22))).rejects.toThrow('CONFLICT');
 const other=new PerspectiveApplyStore(disk,'https://studio.test',id(51),id(1),id(2),id(6),lock);expect(await other.read()).toBeNull();
});
it('logout fences old instances and removes persisted attempts',async()=>{
 const disk=storage(),store=new PerspectiveApplyStore(disk,'https://studio.test',id(50),id(1),id(2),id(6),lock);
 await PerspectiveApplyAttempt.create(fixture(),false,null,store);
 await clearPerspectiveApplyAttempts(disk,'https://studio.test',lock);
 await expect(store.read()).rejects.toThrow('EXPIRED');
 const fresh=new PerspectiveApplyStore(disk,'https://studio.test',id(50),id(1),id(2),id(6),lock);expect(await fresh.read()).toBeNull();
});
