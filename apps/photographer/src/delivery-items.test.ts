import {expect,it,vi} from 'vitest';
import {collectDeliveryItems,readDeliveryItemsPage} from './delivery-items';
import type {DeliveryPackage} from './delivery-package';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const pkg:DeliveryPackage={created_at:'2026-10-10T11:00:00Z',updated_at:'2026-10-10T12:00:00Z',id:id(1),tenant_id:id(2),project_id:id(3),revision:'2',workflow_id:id(4),snapshot_id:id(5),name:'Delivery',policy_artifact_id:id(6),manifest_artifact_id:id(7),document:{artifact_id:id(7),sha256:'a'.repeat(64)},state:'review',published_at:null};
const item=(ordinal:number)=>({id:id(10+ordinal),ordinal,preset_id:id(8),preset_sha:'b'.repeat(64),required:true,export_id:id(100+ordinal),state:'ready',reason:null});
const page=(ordinals:number[],more=false)=>({package_id:pkg.id,revision:pkg.revision,manifest:pkg.document,items:ordinals.map(item),next:more?{ordinal:ordinals[ordinals.length-1],preset_id:id(8)}:null});
it('collects ordered pages using the exact returned cursor and owns records',async()=>{
 const first=page([0,1],true),second=page([2]),load=vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(second);
 const result=await collectDeliveryItems(pkg,load);expect(result.map(v=>v.ordinal)).toEqual([0,1,2]);
 expect(load.mock.calls).toEqual([[null],[{ordinal:1,preset_id:id(8)}]]);
 first.items[0].preset_sha='changed';expect(result[0].preset_sha).toBe('b'.repeat(64));
});
it('refuses revision/manifest change, unsorted records, invalid omission and cursor regression',()=>{
 for(const change of [{revision:'3'},{package_id:id(9)},{manifest:{artifact_id:id(7),sha256:'c'.repeat(64)}},{next:{ordinal:1,preset_id:id(8)}}])expect(()=>readDeliveryItemsPage({...page([0]),...change},pkg,null)).toThrow();
 expect(()=>readDeliveryItemsPage(page([1,0]),pkg,null)).toThrow();
 expect(()=>readDeliveryItemsPage(page([0]),pkg,{ordinal:0,preset_id:id(8)})).toThrow();
 expect(()=>readDeliveryItemsPage({...page([0]),items:[{...item(0),state:'omitted',reason:'policy'}]},pkg,null)).toThrow();
 expect(()=>readDeliveryItemsPage({...page([0]),items:[{...item(0),export_id:null}]},pkg,null)).toThrow();
 expect(()=>readDeliveryItemsPage(page([],true),pkg,null)).toThrow();
});
it('does not return partial results after page failure and rejects cross-page duplicate identity',async()=>{
 const load=vi.fn().mockResolvedValueOnce(page([0],true)).mockRejectedValueOnce(new Error('stale'));
 await expect(collectDeliveryItems(pkg,load)).rejects.toThrow('stale');
 const next=page([1]);next.items[0].id=item(0).id;
 await expect(collectDeliveryItems(pkg,vi.fn().mockResolvedValueOnce(page([0],true)).mockResolvedValueOnce(next))).rejects.toThrow('DELIVERY_ITEMS_INVALID');
});
it('cancels before IO and after a response without loading another page',async()=>{
 const controller=new AbortController(),load=vi.fn();controller.abort();
 await expect(collectDeliveryItems(pkg,load,controller.signal)).rejects.toThrow('DELIVERY_ABORTED');expect(load).not.toHaveBeenCalled();
 const late=new AbortController(),lateLoad=vi.fn(async()=>{late.abort();return page([0],true)});
 await expect(collectDeliveryItems(pkg,lateLoad,late.signal)).rejects.toThrow('DELIVERY_ABORTED');expect(lateLoad).toHaveBeenCalledTimes(1);
});
it('OwnerAPI sends original revision and cursor with cancellation',async()=>{
 const request=vi.fn().mockResolvedValueOnce(page([0],true)).mockResolvedValueOnce(page([1])),signal=new AbortController().signal;
 const result=await new OwnerAPI({request} as never,pkg.tenant_id).deliveryItems(pkg,signal);
 expect(result).toHaveLength(2);
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${pkg.tenant_id}/delivery-packages/${pkg.id}/items?revision=2&limit=200`,{method:'GET',signal}]);
 expect(request.mock.calls[1][0]).toContain(`after_ordinal=0&after_preset=${id(8)}`);
});


it.each([null,'','   '])('rejects failed rows without a meaningful reason %j',reason=>{
 expect(()=>readDeliveryItemsPage({...page([0]),items:[{...item(0),state:'failed',export_id:null,reason}]},pkg,null)).toThrow('DELIVERY_ITEMS_INVALID');
});
it('keeps original cursor when page loader mutates its request argument',async()=>{
 let count=0;
 const load=async(after:{ordinal:number;preset_id:string}|null)=>{
  if(++count===1)return page([0],true);
  expect(after).toEqual({ordinal:0,preset_id:id(8)});after!.ordinal=4999;after!.preset_id=id(999);
  return page([1]);
 };
 expect((await collectDeliveryItems(pkg,load)).map(value=>value.ordinal)).toEqual([0,1]);
});
it('counts reason length by Unicode code points like SQL text length',()=>{
 const reason='😀'.repeat(256);
 const result=readDeliveryItemsPage({...page([0]),items:[{...item(0),state:'failed',export_id:null,reason}]},pkg,null);
 expect(result.items[0].reason).toBe(reason);
 expect(()=>readDeliveryItemsPage({...page([0]),items:[{...item(0),state:'failed',export_id:null,reason:reason+'x'}]},pkg,null)).toThrow();
});

it.each([[false,false],[false,true],[true,false],[true,true]])('omitted required=%s export=%s follows slot contract',(required,hasExport)=>{
 const value={...page([0]),items:[{...item(0),required,export_id:hasExport?id(100):null,state:'omitted',reason:'OPTIONAL_FAILED'}]};
 if(!required&&!hasExport)expect(readDeliveryItemsPage(value,pkg,null).items).toHaveLength(1);
 else expect(()=>readDeliveryItemsPage(value,pkg,null)).toThrow('DELIVERY_ITEMS_INVALID');
});
