import {expect,it} from 'vitest';
import {multiCollectionSelection} from './multi-collection-selection';
import type {Collection,CollectionItem} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const collection={id:id(1),revision:'9'} as Collection;
const item={id:id(2),collection_id:id(1),asset_id:id(3),asset_version_id:id(4)} as CollectionItem;
it('freezes exact occurrence and collection revision without borrowing item state',()=>{
 const result=multiCollectionSelection(collection,[item],id(4));
 expect(result).toEqual({collection:id(1),item:id(2),asset:id(3),revision:'9',expectedVersion:id(4)});
 item.asset_id=id(5);expect(result?.asset).toBe(id(3));item.asset_id=id(3);
});
it('refuses missing, foreign and ambiguous reference occurrences',()=>{
 expect(multiCollectionSelection(collection,[],id(4))).toBeNull();
 expect(multiCollectionSelection(collection,[{...item,collection_id:id(9)}],id(4))).toBeNull();
 expect(multiCollectionSelection(collection,[item,{...item,id:id(8)}],id(4))).toBeNull();
});
it('rejects malformed CAS revision before exposing Apply',()=>{
 expect(()=>multiCollectionSelection({...collection,revision:'0'},[item],id(4))).toThrow();
 expect(()=>multiCollectionSelection({...collection,revision:'9223372036854775808'},[item],id(4))).toThrow();
});
