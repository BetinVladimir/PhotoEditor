import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {savedPreparedReviewCursor} from './interchange-prepared-review';

const id=(n:number)=>`00000000-0000-0000-0000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:String(n).padStart(64,'a')});
const item=(n:number)=>({entry_id:id(n),target_version_id:id(501),kind:'dust' as const,proposal:ref(600+n),base:ref(502),label_map:null,decision:'skip' as const,accepted_candidate_ids:[],rejected_candidate_ids:[],status:'granted' as const,epoch:1});

it('loads saved import reviews beyond the first 500 using the exact page cursor',async()=>{
 const first=Array.from({length:500},(_,i)=>item(i+1)),second=item(501);
 const cursor=savedPreparedReviewCursor(first[499]);
 const request=vi.fn().mockResolvedValueOnce({workflow_id:id(9),items:first,next_after:cursor}).mockResolvedValueOnce({workflow_id:id(9),items:[second]});
 const api=new OwnerAPI({request} as never,id(7));
 const result=await api.savedPreparedImportReviews(id(8),id(9));
 expect(result).toHaveLength(501);
 expect(request).toHaveBeenCalledTimes(2);
 expect(request.mock.calls[1][0]).toContain(`?after=${encodeURIComponent(cursor)}`);
});

it('loads prepared proposals beyond the first 50 using the exact page cursor',async()=>{
 const candidate={id:'f'.repeat(64),decision:'pending',bbox:{x:10,y:20,width:5,height:6},score:0.9};
 const proposal=(n:number)=>({entry_id:id(n),target_version_id:id(501),kind:'defects' as const,proposal:ref(600+n),base:ref(502),document:{version:'defect_candidates_v1',image:ref(502),label_map:ref(503),canvas:{width:100,height:100},candidates:[candidate]},decision_epoch:0});
 const first=Array.from({length:50},(_,i)=>proposal(i+1)),second=proposal(51);
 const {preparedReviewCursor}=await import('./interchange-prepared-review');
 const cursor=preparedReviewCursor(first[49] as never);
 const request=vi.fn().mockResolvedValueOnce({workflow_id:id(9),items:first,next_after:cursor}).mockResolvedValueOnce({workflow_id:id(9),items:[second]});
 const api=new OwnerAPI({request} as never,id(7));
 const result=await api.preparedImportReviews(id(8),id(9));
 expect(result).toHaveLength(51);
 expect(request.mock.calls[1][0]).toContain(`?after=${encodeURIComponent(cursor)}`);
});

it('exposes one bounded prepared review page to the screen',async()=>{
 const candidate={id:'f'.repeat(64),decision:'pending',bbox:{x:10,y:20,width:5,height:6},score:0.9};
 const proposal={entry_id:id(1),target_version_id:id(501),kind:'defects',proposal:ref(601),base:ref(502),document:{version:'defect_candidates_v1',image:ref(502),label_map:ref(503),canvas:{width:100,height:100},candidates:[candidate]},decision_epoch:0};
 const request=vi.fn().mockResolvedValue({workflow_id:id(9),items:[proposal]});
 const api=new OwnerAPI({request} as never,id(7));
 const page=await api.preparedImportReviewPage(id(8),id(9));
 expect(page.items).toHaveLength(1);
 expect(request).toHaveBeenCalledTimes(1);
});
