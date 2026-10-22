import {expect,it,vi} from 'vitest';
import {collectionGroups,readCollectionGroups} from './collection-culling-groups-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const item={collection_item_id:id(4),item_id:id(5),asset_id:id(6),asset_version_id:id(7),scene_id:'scene',exact_group_id:'duplicate',burst_group_id:'burst',rank:1,score:.8,recommended:true,manual:false,eye_review:false,eye_state:'open'};
const context={review_id:id(8),run_id:id(9),candidate_hash:'a'.repeat(64),state:'pending',total:2,matched:1,unmatched:1,items:[item]};
const page=()=>({collection_id:id(3),revision:'2',contexts:[context],next_cursor:null});
it('preserves exact frozen version and explicit historical unmatched count with detached data',()=>{const raw=page(),out=readCollectionGroups(raw,id(3),'2',id(8));expect(out.contexts[0].unmatched).toBe(1);raw.contexts[0].items[0].asset_version_id=id(99);expect(out.contexts[0].items![0].asset_version_id).toBe(id(7));});
it('refuses forged scopes/counts/duplicate current targets and scalar confusion',()=>{
 for(const raw of [{...page(),collection_id:id(99)},{...page(),revision:'3'},{...page(),contexts:[{...context,unmatched:0}]},{...page(),contexts:[{...context,state:['pending']}]},{...page(),contexts:[{...context,items:[item,item],matched:2,unmatched:0}]}])expect(()=>readCollectionGroups(raw,id(3),'2',id(8))).toThrow();
 expect(()=>readCollectionGroups({...page(),contexts:[{...context,items:null}]},id(3),'2',id(8))).toThrow();expect(()=>readCollectionGroups(page(),id(3),'2')).toThrow();
});
it('pre-abort and invalid identity refuse transport; late abort refuses projection',async()=>{
 const request=vi.fn(),client={request};const stopped=new AbortController();stopped.abort();await expect(collectionGroups(client,id(1),id(3),'2',stopped.signal)).rejects.toThrow();await expect(collectionGroups(client,id(1),'bad','2',new AbortController().signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();const late=new AbortController();request.mockImplementation(async()=>{late.abort();return page()});await expect(collectionGroups(client,id(1),id(3),'2',late.signal,{review:id(8)})).rejects.toThrow();
});
