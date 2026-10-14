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
