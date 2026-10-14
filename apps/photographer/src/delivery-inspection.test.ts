import {expect,it,vi} from 'vitest';
import {readDeliveryInspection} from './delivery-inspection';
import type {DeliveryItem} from './delivery-items';
import type {DeliveryPackage} from './delivery-package';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const pkg:DeliveryPackage={id:id(1),tenant_id:id(2),project_id:id(3),revision:'2',workflow_id:id(4),snapshot_id:id(5),name:'Package',policy_artifact_id:id(6),manifest_artifact_id:id(7),document:{artifact_id:id(7),sha256:'a'.repeat(64)},state:'review',published_at:null};
const items:DeliveryItem[]=[{id:id(8),ordinal:0,preset_id:id(9),preset_sha:'b'.repeat(64),required:true,export_id:id(10),state:'ready',reason:null},{id:id(11),ordinal:1,preset_id:id(9),preset_sha:'b'.repeat(64),required:false,export_id:null,state:'omitted',reason:'optional export unavailable'}];
const proof=()=>({package_id:pkg.id,revision:pkg.revision,manifest:{...pkg.document!},slot_count:2,ready_count:1,omitted_count:1});
it('binds complete ready/optional omission set to immutable manifest and owns proof',()=>{
 const wire=proof(),result=readDeliveryInspection(wire,pkg,items);wire.manifest.sha256='changed';expect(result.manifest.sha256).toBe('a'.repeat(64));
});
it('refuses foreign/stale proof, count mismatch, partial items and unknown fields',()=>{
 for(const patch of [{package_id:id(12)},{revision:'3'},{manifest:{artifact_id:id(7),sha256:'c'.repeat(64)}},{slot_count:3},{ready_count:2},{omitted_count:0},{extra:true}])expect(()=>readDeliveryInspection({...proof(),...patch},pkg,items)).toThrow();
 expect(()=>readDeliveryInspection(proof(),pkg,items.slice(0,1))).toThrow();
 expect(()=>readDeliveryInspection(proof(),{...pkg,published_at:'2026-10-10T12:00:00Z'},items)).toThrow();
 expect(()=>readDeliveryInspection(proof(),pkg,[items[0],{...items[1],required:true}])).toThrow();
 expect(()=>readDeliveryInspection(proof(),pkg,[{...items[0],state:'pending'},items[1]])).toThrow();
 expect(()=>readDeliveryInspection(proof(),pkg,[items[1],items[0]])).toThrow();
});
it('client owns package and displayed item snapshot during inspection IO',async()=>{
 let finish!:(value:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise(resolve=>{finish=resolve}));
 const owned=structuredClone(pkg),selection=structuredClone(items),signal=new AbortController().signal;
 const pending=new OwnerAPI({request} as never,pkg.tenant_id).inspectDelivery(owned,selection,signal);
 owned.revision='3';selection[0].state='pending';finish(proof());expect((await pending).revision).toBe('2');
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${pkg.tenant_id}/delivery-packages/${pkg.id}/inspection?revision=2`,{method:'GET',signal}]);
});
it('refuses foreign scope and cancellation before and after IO',async()=>{
 const controller=new AbortController();controller.abort();const request=vi.fn(async()=>proof()),api=new OwnerAPI({request} as never,pkg.tenant_id);
 await expect(api.inspectDelivery(pkg,items,controller.signal)).rejects.toThrow('DELIVERY_ABORTED');
 await expect(api.inspectDelivery({...pkg,tenant_id:id(12)},items)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 const late=new AbortController(),lateRequest=vi.fn(async()=>{late.abort();return proof()});
 await expect(new OwnerAPI({request:lateRequest} as never,pkg.tenant_id).inspectDelivery(pkg,items,late.signal)).rejects.toThrow('DELIVERY_ABORTED');
});
