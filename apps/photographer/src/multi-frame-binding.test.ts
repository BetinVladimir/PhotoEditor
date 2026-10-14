import {describe,it,expect} from 'vitest';
import {loadMultiFrameBinding} from './multi-frame-binding';
import type {CollectionItem} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const binding=()=>({asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)});
const item=()=>({asset_id:id(1),asset_version_id:id(2)} as CollectionItem);
describe('multi source selection',()=>{
 it('owns selected source and refuses a newer current asset version',async()=>{
  const selected=item(),source=binding(),controller=new AbortController();
  const result=await loadMultiFrameBinding({v2Binding:async asset=>{expect(asset).toBe(id(1));return source;}},selected,controller.signal);
  source.source.sha256='c'.repeat(64);expect(result.source.sha256).toBe('a'.repeat(64));
  await expect(loadMultiFrameBinding({v2Binding:async()=>({...binding(),asset_version_id:id(4)})},selected,controller.signal)).rejects.toThrow('MULTI_SELECTION_SOURCE_CHANGED');
 });
 it('captures version before IO and refuses a late response after cancellation',async()=>{
  const selected=item(),controller=new AbortController();
  const result=await loadMultiFrameBinding({v2Binding:async()=>{selected.asset_version_id=id(4);return binding();}},selected,controller.signal);expect(result.asset_version_id).toBe(id(2));
  await expect(loadMultiFrameBinding({v2Binding:async()=>{controller.abort();return binding();}},item(),controller.signal)).rejects.toThrow();
 });
});
