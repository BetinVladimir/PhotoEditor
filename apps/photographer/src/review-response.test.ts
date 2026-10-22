import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {readReviewPage,readReviewView,readReviewDecision} from './review-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(1),run_id:id(2),kind:'defects',state:'pending',revision:1,candidate_hash:'a'.repeat(64),reasons:[],created_at:'2026-10-17T00:00:00Z'});
const accepted=()=>({...row(),state:'decided',revision:2,decision:'reject',decision_route:'base',decision_hash:'b'.repeat(64)});
it('decodes actual list View without inventing detail artifacts; enforces page and filter binding',()=>{
 expect(readReviewPage({items:[row()],next_cursor:null},{state:'pending'}).items).toEqual([row()]);
 for(const value of [{items:[row()]},{items:[row(),row()],next_cursor:null},{items:[{...row(),run_id:id(3)}],next_cursor:null}])expect(()=>readReviewPage(value,{run:id(2)})).toThrow();
 for(const bad of [{...row(),revision:1.5},{...row(),decision:null},{...row(),candidate_hash:'bad'},{...row(),run_id:'bad'}])expect(()=>readReviewView(bad)).toThrow();
});
it('binds durable decision receipt including hash rather than accepting an unrelated or partial View',()=>{
 expect(readReviewDecision(accepted(),row(),'reject')).toEqual(accepted());
 for(const bad of [{...accepted(),revision:1},{...accepted(),run_id:id(3)},{...accepted(),decision:'accept'},{...accepted(),decision_hash:undefined},{...accepted(),decision_route:undefined}])expect(()=>readReviewDecision(bad,row(),'reject')).toThrow();
});
it('freezes decision identity and candidate arrays, validates actual response and cancelled read boundaries',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise<unknown>(r=>{resolve=r})),api=new OwnerAPI({request} as never,id(4));
 const before=row(),selected=['candidate-1'],pending=api.decide(before,'reject',[],selected);before.id=id(9);before.revision=7;selected.push('candidate-2');resolve(accepted());expect((await pending).decision_hash).toBe('b'.repeat(64));expect(request.mock.calls[0]).toMatchObject([expect.stringContaining(id(1)),{body:{expected_revision:1,rejected_candidate_ids:['candidate-1']}}]);
 const abort=new AbortController(),late=api.reviews('pending',undefined,abort.signal);abort.abort();resolve({items:[row()],next_cursor:null});await expect(late).rejects.toMatchObject({name:'AbortError'});await expect(api.review(id(1),abort.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(2);
});
it.each(['preview','culling-items','culling-decision'] as const)('%s fences actual transport before and after cancellation',async kind=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise<unknown>(r=>{resolve=r})),api=new OwnerAPI({request} as never,id(4)),abort=new AbortController();
 const call=()=>kind==='preview'?api.reviewPreview(id(1),'before',abort.signal):kind==='culling-items'?api.cullingItems({...row(),kind:'culling'},null,abort.signal):api.decideCulling(id(1),{expected_revision:1,candidate_hash:'a'.repeat(64),selected_item_ids:[id(3)],confirmed_eyes_item_ids:[],never_delete:true},id(5),abort.signal);
 const pending=call();abort.abort();resolve({});await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(call()).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});

it('requires Go time wire shape without changing optional historical summary fields',()=>{
 for(const created_at of ['2026-10-17T00:00:00.123456789Z','2026-10-17T02:00:00+02:00'])expect(readReviewView({...row(),created_at}).created_at).toBe(created_at);
 for(const created_at of ['1','2026-10-17','2026-10-17T00:00Z','2026-10-17T00:00:00','2026-10-17 00:00:00Z'])expect(()=>readReviewView({...row(),created_at})).toThrow('REVIEW_RESPONSE_INVALID');
 expect(readReviewView({...accepted(),decision_route:undefined,decision_hash:undefined})).toMatchObject({state:'decided',decision:'reject'});
});
