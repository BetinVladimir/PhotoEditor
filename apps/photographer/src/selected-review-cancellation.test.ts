import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import type {Review} from './catalog-types';
import {OwnerAPI} from './owner-api';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const image={artifact_id:id(3),sha256:'a'.repeat(64)};
const makeReview=(kind:string):Review=>({id:id(1),run_id:id(2),kind,state:'decided',revision:2,candidate_hash:image.sha256,candidate:image,base:{artifact_id:id(4),sha256:'b'.repeat(64)},decision:'accept',decision_route:'candidate',decision_hash:'c'.repeat(64),reasons:[],created_at:'2026-10-17T00:00:00Z'});
const cases=[
 ['negative','optical.negative',(api:OwnerAPI,r:Review,s:AbortSignal)=>api.negativeResultSelection(r,s)],
 ['render','render.compose',(api:OwnerAPI,r:Review,s:AbortSignal)=>api.renderResultSelection(r,s)],
 ['restoration','portrait.glare',(api:OwnerAPI,r:Review,s:AbortSignal)=>api.restorationResultSelection(r,s)],
 ['render preview','render.compose',(api:OwnerAPI,r:Review,s:AbortSignal)=>api.renderSelectedPreview(r,s)],
 ['restoration preview','portrait.glare',(api:OwnerAPI,r:Review,s:AbortSignal)=>api.restorationSelectedPreview(r,s)],
] as const;

it.each(cases)('refuses pre-cancelled %s lookup before transport',async(_name,kind,lookup)=>{
 const abort=new AbortController();abort.abort();
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(lookup(api,makeReview(kind),abort.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(request).not.toHaveBeenCalled();
});

it.each(cases)('does not expose late %s lookup after cancellation',async(_name,kind,lookup)=>{
 const abort=new AbortController(),review=makeReview(kind);
 const request=vi.fn().mockImplementation(async()=>{abort.abort();return {schema_version:1,review_id:review.id,decision_hash:review.decision_hash,route:'candidate',image};});
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(lookup(api,review,abort.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(request).toHaveBeenCalledTimes(1);
});

it.each(cases.slice(0,3))('keeps the immutable %s selection binding during transport',async(_name,kind,lookup)=>{
 const review=makeReview(kind),expected={schema_version:1,review_id:review.id,decision_hash:review.decision_hash,route:'candidate',image:{...image}};
 const request=vi.fn().mockImplementation(async()=>{review.candidate!.sha256='d'.repeat(64);review.decision_hash='e'.repeat(64);return expected;});
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(lookup(api,review,new AbortController().signal)).resolves.toEqual(expected);
});
