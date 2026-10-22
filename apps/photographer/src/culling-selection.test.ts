import {expect,it,vi} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {loadCullingSelection,cullingDecisionIntent,cullingDecisionReceipt,recoverCullingDecision,readCullingPage,type CullingPage,type CullingSelection} from './culling-selection';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const review={id:id(1),run_id:id(2),kind:'culling',state:'pending',revision:1,candidate_hash:sha,reasons:[],created_at:'2026-10-17T00:00:00Z'};
function item(n:number){return {item_id:id(n+100),asset_id:id(n+1000),asset_version_id:id(n+2000),scene_id:'scene',exact_group_id:'exact',burst_group_id:'burst',rank:n+1,score:.8,recommended:true,manual:false,eye_review:false,eye_state:'open' as const};}
function page(total=1):CullingPage{return {review_id:review.id,run_id:review.run_id,revision:1,state:'pending',candidate_hash:sha,snapshot_id:id(3),snapshot_digest:sha,never_delete:true,total,items:[item(0)],next_cursor:null,groups:{artifact_id:id(4),sha256:sha},proposal:{artifact_id:id(5),sha256:sha},selected_item_ids:null,confirmed_eyes_item_ids:null};}
function selection():CullingSelection{const {next_cursor,...s}=page();return s;}
it('loads every target before enabling a photographer selection',async()=>{
 const first={...page(101),items:Array.from({length:100},(_,n)=>item(n)),next_cursor:'opaque-100'},last={...page(101),items:[item(100)]};
 const cullingItems=vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(last),result=await loadCullingSelection({cullingItems},review,new AbortController().signal);
 expect(result.items).toHaveLength(101);expect(cullingItems.mock.calls[1][1]).toBe('opaque-100');expect(result.never_delete).toBe(true);
});
it('rejects duplicate targets, cursor loops, incomplete inventories and changed immutable pins',async()=>{
 const first={...page(101),items:Array.from({length:100},(_,n)=>item(n)),next_cursor:'opaque-100'};
 for(const last of [{...page(101),items:[item(0)]},{...page(101),snapshot_digest:'b'.repeat(64),items:[item(100)]},{...first,items:Array.from({length:100},(_,n)=>item(n+100))}]){
  const api={cullingItems:vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(last)};
  await expect(loadCullingSelection(api,review,new AbortController().signal)).rejects.toThrow('INVALID');
 }
 await expect(loadCullingSelection({cullingItems:vi.fn(async()=>page(2))},review,new AbortController().signal)).rejects.toThrow('INVALID');
});
it('requires explicit confirmation for each selected uncertain eye state, preserves nonselected originals',()=>{
 const s=selection();s.items[0].eye_state='unknown';s.items[0].eye_review=true;
 expect(()=>cullingDecisionIntent(id(6),id(7),s,[s.items[0].item_id],[])).toThrow('INVALID');
 const intent=cullingDecisionIntent(id(6),id(7),s,[s.items[0].item_id],[s.items[0].item_id]);expect(intent.request.never_delete).toBe(true);
 expect(()=>cullingDecisionIntent(id(6),id(7),s,[],[])).toThrow('INVALID');expect(()=>cullingDecisionIntent(id(6),id(7),s,[id(99)],[])).toThrow('INVALID');
});
it('reconciles an accepted response without posting again and rejects another actor decision',async()=>{
 const s=selection(),intent=cullingDecisionIntent(id(6),id(7),s,[s.items[0].item_id],[]),receipt={...review,state:'decided',revision:2,decision:'accept',decision_route:'selection',decision_hash:intent.decision_hash};
 const api={review:vi.fn(async()=>receipt),decideCulling:vi.fn()};expect(await recoverCullingDecision(api,intent,id(8),new AbortController().signal)).toEqual(receipt);expect(api.decideCulling).not.toHaveBeenCalled();
 const other=cullingDecisionIntent(id(6),id(9),s,[s.items[0].item_id],[]);expect(()=>cullingDecisionReceipt(receipt,other)).toThrow('CONFLICT');
});
it('retains the same request and key after an uncertain dispatch',async()=>{
 const s=selection(),intent=cullingDecisionIntent(id(6),id(7),s,[s.items[0].item_id],[]),api={review:vi.fn(async()=>review),decideCulling:vi.fn().mockRejectedValue(new Error('offline'))};
 await expect(recoverCullingDecision(api,intent,id(8),new AbortController().signal)).rejects.toThrow('offline');
 expect(api.decideCulling).toHaveBeenCalledWith(review.id,intent.request,id(8),expect.any(AbortSignal));expect(intent.request.selected_item_ids).toEqual([s.items[0].item_id]);
});
it('never posts following forbidden receipt lookup and stops reconciliation on denied mutation',async()=>{
 const s=selection(),intent=cullingDecisionIntent(id(6),id(7),s,[s.items[0].item_id],[]);
 for(const status of [401,403]){
  const api={review:vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN')),decideCulling:vi.fn()};await expect(recoverCullingDecision(api,intent,id(8),new AbortController().signal)).rejects.toThrow();expect(api.decideCulling).not.toHaveBeenCalled();
  const denied={review:vi.fn(async()=>review),decideCulling:vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN'))};await expect(recoverCullingDecision(denied,intent,id(8),new AbortController().signal)).rejects.toThrow();expect(denied.review).toHaveBeenCalledTimes(1);
 }
});
it('rejects never_delete false and rebound preview targets',()=>{expect(()=>readCullingPage({...page(),never_delete:false},review)).toThrow();expect(()=>readCullingPage({...page(),run_id:id(99)},review)).toThrow();});
