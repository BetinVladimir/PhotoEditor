import {it,expect} from 'vitest';
import {collectionApplyRequest,readCollectionApplyAccepted} from './collection-apply-contract';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
it('preserves exact safe revision and counts Unicode names as server runes',()=>{
 const c={id:id(1),revision:'9007199254740991'};expect(collectionApplyRequest(c,id(2),.5,'😀'.repeat(120),id(3)).source_revision).toBe(9007199254740991);
 expect(()=>collectionApplyRequest(c,id(2),.5,'😀'.repeat(121),id(3))).toThrow('INVALID');
 for(const revision of ['0','-1','1.5','01','9007199254740992','9'.repeat(1000)])expect(()=>collectionApplyRequest({...c,revision},id(2),.5,'Edited',id(3))).toThrow('INVALID');
});
it('owns only exact queued admission receipts',()=>{
 const receipt={run_id:id(1),output_collection_id:id(2),status:'queued'};const owned=readCollectionApplyAccepted(receipt);receipt.run_id=id(3);expect(owned.run_id).toBe(id(1));
 for(const value of [null,{...owned,status:'running'},{...owned,status:''},{...owned,secret:'extra'},{...owned,run_id:'bad'}])expect(()=>readCollectionApplyAccepted(value)).toThrow('INVALID');
});
