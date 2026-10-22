import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {LensClient} from './lens-client';
import type {PerspectiveIntent} from './perspective-intent';
import type {NegativeIntent} from './negative-intent';
import type {MultiIntent} from './multi-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base=()=>({asset_version_id:id(4),source:ref(5),canvas_hash:'b'.repeat(64)});
const perspective=():PerspectiveIntent=>({schema_version:1,snapshot_id:id(3),params:{schema_version:1,base:base(),strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}});
const negative=():NegativeIntent=>({schema_version:1,snapshot_id:id(3),params:{schema_version:1,base:base(),strength:.5,film_base_rgb:[1,.8,.6],calibration:ref(6),exposure_ev:0}});
const multi=():MultiIntent=>({schema_version:1,snapshot_id:id(3),params:{schema_version:1,frames:[base(),{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)}],reference_ordinal:0,max_pixels:10000,calibration:ref(6),projection:'cylindrical'}});
const cases=['perspective','negative','multi','lens'] as const;
function submit(kind:typeof cases[number],request:ReturnType<typeof vi.fn>,signal:AbortSignal){
 const api=new OwnerAPI({request} as never,id(1));
 if(kind==='perspective')return api.submitPerspective(id(2),perspective(),'stable-key',signal);
 if(kind==='negative')return api.submitNegative(id(2),negative(),'stable-key',signal);
 if(kind==='multi')return api.submitMulti('optical.panorama',id(2),multi(),'stable-key',signal);
 return new LensClient({request} as never,id(1),id(2)).submit({schema_version:1,snapshot_id:id(3),choices:[{asset_version_id:id(4),profile:ref(6),strength:.5}]},[id(4)],'stable-key',signal);
}
it.each(cases)('%s refuses cancelled admission before any transport',async kind=>{
 const request=vi.fn(),controller=new AbortController();controller.abort();
 await expect(submit(kind,request,controller.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(request).not.toHaveBeenCalled();
});
it.each(cases)('%s refuses a valid receipt from an adapter resolving after cancellation',async kind=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{resolve=done;})),controller=new AbortController();
 const pending=submit(kind,request,controller.signal);controller.abort();resolve({run_id:id(10),workflow_id:id(11),state:'queued'});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
it('refuses cancelled replacement scope reads before transport',async()=>{
 const request=vi.fn(),controller=new AbortController();controller.abort();const api=new OwnerAPI({request} as never,id(1));
 await expect(api.multiReplacementScope('optical.panorama',id(2),id(3),id(4),'a'.repeat(64),controller.signal)).rejects.toThrow();
 await expect(api.perspectiveReplacementScope(id(2),id(3),id(4),'a'.repeat(64),controller.signal)).rejects.toThrow();
 await expect(api.perspectiveProposal(id(2),id(3),base(),controller.signal)).rejects.toThrow();
 expect(request).not.toHaveBeenCalled();
});
it('Lens binds the pending descriptor to its original source and root scope',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{resolve=done;}));const client=new LensClient({request} as never,id(1),id(2));
 const original=base(),expected=structuredClone(original),pending=client.proposal(id(1),id(2),original,id(3));original.source.sha256='d'.repeat(64);
 const proposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(9),result:ref(10),base:expected,working_image:ref(11),changed_mask:ref(12),report:ref(13),canvas:{width:100,height:80,orientation:1,space_id:'lens:canvas',source_sha256:'c'.repeat(64)},status:'pass',requires_review:false};
 resolve(proposal);expect((await pending).base).toEqual(expected);
 await expect(client.proposal(id(14),id(2),expected,id(3))).rejects.toThrow('LENS_SCOPE_INVALID');expect(request).toHaveBeenCalledTimes(1);
});
it('Lens Apply owns the selected source while the adapter is pending',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{resolve=done;})),client=new LensClient({request} as never,id(1),id(2));
 const proposal={schema_version:1 as const,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(9),result:ref(10),base:base(),working_image:ref(11),changed_mask:ref(12),report:ref(13),canvas:{width:100,height:80,orientation:1,space_id:'lens:canvas',source_sha256:'c'.repeat(64)},status:'pass' as const,requires_review:false};
 const selected=structuredClone(proposal),pending=client.apply(proposal,false);proposal.working_image.sha256='d'.repeat(64);proposal.base.asset_version_id=id(99);
 resolve({asset_version_id:id(15),parent_asset_version_id:selected.base.asset_version_id,source:selected.working_image});expect((await pending).source).toEqual(selected.working_image);
});
it('perspective descriptor ignores a valid response delivered after cancellation',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{resolve=done;})),api=new OwnerAPI({request} as never,id(1)),controller=new AbortController();
 const pending=api.perspectiveProposal(id(2),id(3),base(),controller.signal);controller.abort();
 resolve({schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(9),result:ref(10),base:base(),working_image:ref(11),changed_mask:ref(12),report:ref(13),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});
});

it('rejects malformed multi run identity before transport even if response could be null',async()=>{
 const request=vi.fn().mockResolvedValue({scope:null}),api=new OwnerAPI({request} as never,id(1));
 await expect(api.multiRunReview('../foreign')).rejects.toThrow('MULTI_RUN_SCOPE_INVALID');expect(request).not.toHaveBeenCalled();
});
