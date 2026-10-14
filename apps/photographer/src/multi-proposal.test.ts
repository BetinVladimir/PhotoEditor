import {describe,it,expect} from 'vitest';
import {readMultiProposal,type MultiProposal} from './multi-proposal';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
import type {MultiIntent,MultiOperation} from './multi-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function fixture(op:MultiOperation){
 const frames=[{asset_version_id:id(20),source:ref(21),canvas_hash:'b'.repeat(64)},{asset_version_id:id(22),source:ref(23),canvas_hash:'c'.repeat(64)}];
 const common={schema_version:1 as const,frames,reference_ordinal:1,max_pixels:10000,calibration:ref(24)};
 const intent:MultiIntent={schema_version:1,snapshot_id:id(25),params:op==='optical.panorama'?{...common,projection:'planar'}:{...common,motion_policy:'review'}};
 const proposal:MultiProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),operation:op,result_id:id(4),result:ref(5),sources:structuredClone(frames),reference_ordinal:1,working_image:ref(6),changed_mask:ref(7),report:ref(8),registrations:[ref(9),ref(10)],canvas:{width:100,height:100,orientation:1,space_id:'multi:'+'d'.repeat(32),source_sha256:'e'.repeat(64)},status:'review',requires_review:true,reason:op.slice(8).toUpperCase()+'_REVIEW;motion_fraction=0.125;REGISTRATION_INCONSISTENT'};
 return {intent,proposal};
}
describe('multi review descriptor',()=>{
 for(const op of ['optical.panorama','optical.focus_stack'] as const){
  it(op+' owns exact ordered bindings and measured reason',()=>{
   const {intent,proposal}=fixture(op),got=readMultiProposal(proposal,id(1),id(2),id(3),op,intent);
   proposal.sources[0].source.sha256='f'.repeat(64);expect(got.sources[0].source.sha256).toBe('a'.repeat(64));expect(got.reason).toContain('0.125');
  });
  it(op+' refuses scope/order/downgrade/alias/extra fields',()=>{
   const {intent,proposal}=fixture(op);
   for(const delta of [{tenant_id:id(30)},{operation:'optical.lens'},{sources:[proposal.sources[1],proposal.sources[0]]},{reference_ordinal:0},{status:'pass',requires_review:false},{registrations:[ref(9),ref(9)]},{report:ref(21)},{url:'untrusted'},{canvas:{...proposal.canvas,width:101}},{reason:op.slice(8).toUpperCase()+'_REVIEW;motion_fraction=NaN'}])expect(()=>readMultiProposal({...proposal,...delta},id(1),id(2),id(3),op,intent)).toThrow();
  });
  it(op+' freezes async request and selects exact route',async()=>{
   const {intent,proposal}=fixture(op);let finish!:(v:unknown)=>void;let path='';let options:unknown;
   const client={request:(p:string,o:unknown)=>{path=p;options=o;return new Promise(resolve=>{finish=resolve;});}} as unknown as ApiClient;
   const controller=new AbortController(),pending=new OwnerAPI(client,id(1)).multiProposal(op,id(2),id(3),intent,controller.signal);
   intent.params.frames.reverse();finish(proposal);expect((await pending).sources[0].asset_version_id).toBe(id(20));expect(path).toContain(op==='optical.panorama'?'/panorama-proposals/':'/focus-stack-proposals/');expect(options).toEqual({method:'GET',signal:controller.signal});
  });
 }
 it('rejects invalid scope before transport',async()=>{
  let calls=0;const client={request:()=>{calls++;}} as unknown as ApiClient;
  await expect(new OwnerAPI(client,id(1)).multiProposal('optical.panorama','../foreign',id(3),fixture('optical.panorama').intent)).rejects.toThrow();expect(calls).toBe(0);
 });
});
