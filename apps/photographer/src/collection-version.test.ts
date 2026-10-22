import {it,expect} from 'vitest';
import {freezeCollectionVersion,readCollectionVersionSelection,reconcileCollectionVersion} from './collection-version';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request={collection:id(1),item:id(2),asset:id(3),revision:'1',expectedVersion:id(4),version:id(5)};
it('owns exact CAS and permits full int64 revision',()=>{const v={...request,revision:'9223372036854775807'},owned=freezeCollectionVersion(v);v.version=id(6);expect(owned.version).toBe(id(5));});
it('rejects invalid or lossy revisions and same version',()=>{for(const revision of ['0','01','+1','1.0','9223372036854775808',''])expect(()=>freezeCollectionVersion({...request,revision})).toThrow();expect(()=>freezeCollectionVersion({...request,version:request.expectedVersion})).toThrow();});
it('rejects foreign item/asset/version response',()=>{const value={id:id(2),collection_id:id(1),asset_id:id(3),asset_version_id:id(5),position:0,visibility:'hidden',moderation_state:'accepted',rating:0,flag:'none',tags:[],created_at:'2026-10-17T12:00:00Z'};expect(readCollectionVersionSelection(value,request).id).toBe(id(2));for(const patch of [{id:id(6)},{collection_id:id(6)},{asset_id:id(6)},{asset_version_id:id(6)}])expect(()=>readCollectionVersionSelection({...value,...patch},request)).toThrow();});

it('reconciles lost response without overwriting newer edits',()=>{
 const item={id:id(2),collection_id:id(1),asset_id:id(3),asset_version_id:id(5),position:0,visibility:'hidden',moderation_state:'accepted',rating:0,flag:'none',tags:[],created_at:'2026-10-17T12:00:00Z'};
 expect(reconcileCollectionVersion({item,collection_revision:'2'},request)).toBe('applied');
 item.asset_version_id=id(4);expect(reconcileCollectionVersion({item,collection_revision:'1'},request)).toBe('unchanged');
 expect(reconcileCollectionVersion({item,collection_revision:'2'},request)).toBe('conflict');
 item.asset_version_id=id(6);expect(reconcileCollectionVersion({item,collection_revision:'1'},request)).toBe('conflict');
});
