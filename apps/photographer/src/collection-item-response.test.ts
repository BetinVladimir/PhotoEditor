import {expect,it} from 'vitest';
import {readCollectionItemResponse} from './collection-item-response';
import {readCollectionVersionSelection,reconcileCollectionVersion} from './collection-version';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const expected={collection:id(1),item:id(2),asset:id(3),version:id(4)};
const item=()=>({id:id(2),collection_id:id(1),asset_id:id(3),asset_version_id:id(4),position:0,visibility:'hidden',moderation_state:'accepted',rating:0,flag:'none',tags:['portrait'],created_at:'2026-10-17T12:00:00Z'});
it('validates the entire server item before returning an owned receipt',()=>{
 const raw=item(),owned=readCollectionItemResponse(raw,expected);raw.tags.push('late');expect(owned.tags).toEqual(['portrait']);
 for(const patch of [{id:id(9)},{collection_id:id(9)},{asset_id:id(9)},{asset_version_id:id(9)},{rating:6},{visibility:'public'},{moderation_state:'ready'},{tags:null},{section_id:null},{color_label:null},{created_at:null},{bucket:'private'}])expect(()=>readCollectionItemResponse({...item(),...patch},expected)).toThrow();
 const partial={id:id(2),collection_id:id(1),asset_id:id(3),asset_version_id:id(4)};expect(()=>readCollectionItemResponse(partial,expected)).toThrow();
});
it('rejects a full but wrong moderation result',()=>{
 expect(readCollectionItemResponse(item(),{...expected,moderation:'accepted'}).moderation_state).toBe('accepted');
 expect(()=>readCollectionItemResponse(item(),{...expected,moderation:'rejected'})).toThrow();
});
it('does not acknowledge a version receipt or GET reconciliation on a partial/expanded DTO',()=>{
 const request={...expected,revision:'7',expectedVersion:id(5)};
 expect(readCollectionVersionSelection(item(),request).id).toBe(id(2));
 expect(reconcileCollectionVersion({item:item(),collection_revision:'8'},request)).toBe('applied');
 expect(()=>readCollectionVersionSelection({...item(),object_key:'private'},request)).toThrow();
 expect(()=>reconcileCollectionVersion({item:item(),collection_revision:'8',extra:true},request)).toThrow();
 expect(()=>reconcileCollectionVersion({item:{id:id(2),collection_id:id(1),asset_id:id(3),asset_version_id:id(4)},collection_revision:'8'},request)).toThrow();
});
