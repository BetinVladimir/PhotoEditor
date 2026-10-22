import {expect,it,vi} from 'vitest';
import type {Review} from './catalog-types';
import {relightReviewDecisionIntent,recoverRelightReviewDecision} from './relight-review-decision';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){
 const review:Review={id:id(1),run_id:id(2),kind:'image.relight',state:'pending',revision:1,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},reasons:[],created_at:'2026-10-16T00:00:00Z'};
 const intent=relightReviewDecisionIntent(id(5),id(6),review,'accept');
 const decided:Review={...review,state:'decided',revision:2,decision:'accept',decision_route:'candidate',decision_hash:intent.decision_hash};
 return {review,intent,decided};
}
it('recovers the existing actor-bound decision without mutating again',async()=>{
 const f=fixture(),api={review:vi.fn().mockResolvedValue(f.decided),decide:vi.fn()};
 expect(await recoverRelightReviewDecision(api,f.intent,id(7))).toEqual(f.decided);
 expect(api.decide).not.toHaveBeenCalled();
});
it('reads the committed receipt after a lost mutation response',async()=>{
 const f=fixture(),api={review:vi.fn().mockResolvedValueOnce(f.review).mockResolvedValueOnce(f.decided),decide:vi.fn().mockRejectedValue(new Error('network lost'))};
 expect(await recoverRelightReviewDecision(api,f.intent,id(7))).toEqual(f.decided);
 expect(api.decide).toHaveBeenCalledTimes(1);
 expect(api.decide).toHaveBeenCalledWith(f.review,'accept',[],[],id(7),undefined);
});
it('reloads detail after a summary response',async()=>{
 const f=fixture(),{candidate,base,...summary}=f.decided;
 const api={review:vi.fn().mockResolvedValueOnce(f.review).mockResolvedValueOnce(f.decided),decide:vi.fn().mockResolvedValue(summary)};
 expect(await recoverRelightReviewDecision(api,f.intent,id(7))).toEqual(f.decided);
 expect(api.decide).toHaveBeenCalledTimes(1);
});
it('refuses changed source binding before any mutation',async()=>{
 const f=fixture(),api={review:vi.fn().mockResolvedValue({...f.review,base:{artifact_id:id(9),sha256:'c'.repeat(64)}}),decide:vi.fn()};
 await expect(recoverRelightReviewDecision(api,f.intent,id(7))).rejects.toThrow('SCOPE_CHANGED');
 expect(api.decide).not.toHaveBeenCalled();
});
it('refuses another actor decision even with the same selected route',async()=>{
 const f=fixture(),other=relightReviewDecisionIntent(id(5),id(9),f.review,'accept');
 const api={review:vi.fn().mockResolvedValue({...f.decided,decision_hash:other.decision_hash}),decide:vi.fn()};
 await expect(recoverRelightReviewDecision(api,f.intent,id(7))).rejects.toThrow('CONFLICT');
 expect(api.decide).not.toHaveBeenCalled();
});
it('retains an unresolved failure without a second mutation',async()=>{
 const f=fixture(),failure=new Error('network lost'),api={review:vi.fn().mockResolvedValue(f.review),decide:vi.fn().mockRejectedValue(failure)};
 await expect(recoverRelightReviewDecision(api,f.intent,id(7))).rejects.toBe(failure);
 expect(api.decide).toHaveBeenCalledTimes(1);
});
it('does not mutate when cancelled during the initial read',async()=>{
 const f=fixture(),controller=new AbortController();
 const api={review:vi.fn().mockImplementation(async()=>{controller.abort();return f.review}),decide:vi.fn()};
 await expect(recoverRelightReviewDecision(api,f.intent,id(7),controller.signal)).rejects.toThrow();
 expect(api.decide).not.toHaveBeenCalled();
});

it.each([['accept','candidate'],['reject','base'],['skip','skip']] as const)('keeps %s mapped to its immutable %s receipt',async(decision,route)=>{
 const f=fixture(),intent=relightReviewDecisionIntent(id(5),id(6),f.review,decision);
 const decided:Review={...f.review,state:'decided',revision:2,decision,decision_route:route,decision_hash:intent.decision_hash};
 const api={review:vi.fn().mockResolvedValueOnce(f.review).mockResolvedValueOnce(decided),decide:vi.fn().mockResolvedValue(decided)};
 expect(await recoverRelightReviewDecision(api,intent,id(7))).toEqual(decided);
 expect(api.decide).toHaveBeenCalledWith(f.review,decision,[],[],id(7),undefined);
});
it('refuses a mismatched selected route even when the hash matches',async()=>{
 const f=fixture(),api={review:vi.fn().mockResolvedValue({...f.decided,decision_route:'base'}),decide:vi.fn()};
 await expect(recoverRelightReviewDecision(api,f.intent,id(7))).rejects.toThrow('Invalid candidate review binding');
 expect(api.decide).not.toHaveBeenCalled();
});
