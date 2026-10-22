import {expect,it} from 'vitest';
import {readRelightCollectionTargets} from './relight-collection-targets';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const target={collection:id(1),item:id(2),asset:id(3),revision:'7',expectedVersion:id(4),name:'Portraits'};
it('accepts a bounded collection choice and an empty page',()=>{
 expect(readRelightCollectionTargets({items:[target],next_cursor:null}).items).toEqual([target]);
 expect(readRelightCollectionTargets({items:[],next_cursor:null}).items).toEqual([]);
});
it('rejects duplicate items, invalid revisions and foreign continuation cursors',()=>{
 for(const value of [{items:[target,target],next_cursor:null},{items:[{...target,revision:'07'}],next_cursor:null},{items:[target],next_cursor:id(9)},{items:[{...target,unexpected:true}],next_cursor:null}])expect(()=>readRelightCollectionTargets(value)).toThrow();
});
