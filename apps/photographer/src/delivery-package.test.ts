import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {freezeDeliveryPublish,prepareDeliveryPublication,readDeliveryPackage,readPublishedDelivery} from './delivery-package';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const intent=()=>({id:id(1),project_id:id(3),expected_revision:'1'});
function resource(){return {id:id(1),revision:'2',document:{artifact_id:id(7),sha256:'a'.repeat(64)},data:{resource_type:'delivery_packages',id:id(1),tenant_id:id(2),project_id:id(3),created_at:'2026-10-10T11:00:00Z',revision:'2',updated_at:'2026-10-10T12:00:00Z',workflow_id:id(4),snapshot_id:id(5),name:'Client delivery',policy_artifact_id:id(6),manifest_artifact_id:id(7),state:'ready',published_at:'2026-10-10T12:00:00Z'}};}
it('owns scoped metadata without exposing arbitrary server fields',()=>{
 const wire=resource(),value=readDeliveryPackage(wire,id(2),id(3),id(1));
 wire.document.sha256='b'.repeat(64);wire.data.name='changed';
 expect(value.document?.sha256).toBe('a'.repeat(64));expect(value.name).toBe('Client delivery');
 expect('resource_type' in value).toBe(false);
});
it('rejects foreign scope, envelope disagreement and manifest substitution',()=>{
 for(const patch of [{tenant_id:id(8)},{project_id:id(8)},{id:id(8)},{revision:'3'},{manifest_artifact_id:id(8)},{state:'unknown'},{revision:'9007199254740992'}]){
  const wire=resource();Object.assign(wire.data,patch);expect(()=>readDeliveryPackage(wire,id(2),id(3),id(1))).toThrow();
 }
 const wire=resource();wire.document.sha256='wrong';expect(()=>readDeliveryPackage(wire,id(2),id(3),id(1))).toThrow();
 expect(()=>readDeliveryPackage({...resource(),object_key:'private'},id(2),id(3),id(1))).toThrow();
 const expanded=resource() as ReturnType<typeof resource>&{data:ReturnType<typeof resource>['data']&{bucket?:string}};
 expanded.data.bucket='private';expect(()=>readDeliveryPackage(expanded,id(2),id(3),id(1))).toThrow();
 const missing=resource() as ReturnType<typeof resource>;
 delete (missing.data as Partial<typeof missing.data>).created_at;
 expect(()=>readDeliveryPackage(missing,id(2),id(3),id(1))).toThrow();
 const badTime=resource();badTime.data.updated_at='not-a-date';
 expect(()=>readDeliveryPackage(badTime,id(2),id(3),id(1))).toThrow();
 for(const time of ['2026','2026-02-30T12:00:00Z','2026-10-10T25:00:00Z','2026-10-10T12:00:00+24:00']){
  const malformed=resource();malformed.data.updated_at=time;
  expect(()=>readDeliveryPackage(malformed,id(2),id(3),id(1))).toThrow();
 }
 const reversed=resource();reversed.data.updated_at='2026-10-10T10:59:59Z';
 expect(()=>readDeliveryPackage(reversed,id(2),id(3),id(1))).toThrow();
 const offset=resource();offset.data.updated_at='2026-10-10T14:00:00+02:00';
 expect(readDeliveryPackage(offset,id(2),id(3),id(1)).id).toBe(id(1));
});
it('allows unavailable document for metadata but refuses publication confirmation',()=>{
 const wire={...resource(),document:null};
 expect(readDeliveryPackage(wire,id(2),id(3),id(1)).document).toBeNull();
 expect(()=>readPublishedDelivery(wire,id(2),intent())).toThrow('DELIVERY_PUBLICATION_MISMATCH');
 for(const patch of [{state:'review',published_at:null},{published_at:null},{revision:'3'}]){
  const value=resource();Object.assign(value.data,patch);if('revision' in patch)value.revision=patch.revision!;
  expect(()=>readPublishedDelivery(value,id(2),intent())).toThrow();
 }
 expect(readPublishedDelivery(resource(),id(2),intent()).revision).toBe('2');
});
it('freezes original CAS and accepts exactly the shared ASCII key domain',()=>{
 const request=intent(),owned=freezeDeliveryPublish(request,'key!~');request.expected_revision='3';expect(owned.expected_revision).toBe('1');
 for(const expected_revision of ['0','01','+1','1.0','9007199254740991','9007199254740992'])expect(()=>freezeDeliveryPublish({...intent(),expected_revision},'key')).toThrow();
 for(const key of ['', ' space','key ', 'a'.repeat(129),'é','key\n'])expect(()=>freezeDeliveryPublish(intent(),key)).toThrow();
 expect(()=>freezeDeliveryPublish({...intent(),extra:true} as never,'key')).toThrow();
});
it('sends only CAS with stable idempotency key and keeps original intent during IO',async()=>{
 let finish!:(value:unknown)=>void;
 const request=vi.fn((..._args:unknown[])=>new Promise(resolve=>{finish=resolve}));
 const api=new OwnerAPI({request} as never,id(2)),body=intent(),signal=new AbortController().signal;
 const pending=api.publishDelivery(body,'stable-key',signal);body.expected_revision='9';body.project_id=id(8);
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${id(2)}/delivery-packages/${id(1)}/publish`,{method:'POST',body:{expected_revision:'1'},idempotencyKey:'stable-key',signal}]);
 finish(resource());expect((await pending).revision).toBe('2');
});
it('cancellation before request performs no IO and cancellation after response refuses success',async()=>{
 const controller=new AbortController(),request=vi.fn(async()=>resource()),api=new OwnerAPI({request} as never,id(2));controller.abort();
 await expect(api.publishDelivery(intent(),'key',controller.signal)).rejects.toThrow('DELIVERY_ABORTED');
 await expect(api.deliveryPackage(id(3),id(1),controller.signal)).rejects.toThrow('DELIVERY_ABORTED');expect(request).not.toHaveBeenCalled();
 const late=new AbortController(),lateRequest=vi.fn(async()=>{late.abort();return resource()});
 await expect(new OwnerAPI({request:lateRequest} as never,id(2)).publishDelivery(intent(),'key',late.signal)).rejects.toThrow('DELIVERY_ABORTED');
});
it('validates scope before IO and binds GET metadata to project',async()=>{
 const request=vi.fn(async()=>resource()),api=new OwnerAPI({request} as never,id(2));
 await expect(api.deliveryPackage('bad',id(1))).rejects.toThrow();
 await expect(new OwnerAPI({request} as never,'bad').publishDelivery(intent(),'key')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 expect((await api.deliveryPackage(id(3),id(1))).id).toBe(id(1));
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${id(2)}/delivery-packages/${id(1)}`,{method:'GET',signal:undefined}]);
 await expect(api.deliveryPackage(id(8),id(1))).rejects.toThrow();
});
it('resolves a created delivery by workflow and refuses substituted workflow',async()=>{
 const request=vi.fn(async()=>resource()),api=new OwnerAPI({request} as never,id(2));
 expect((await api.deliveryPackageForWorkflow(id(3),id(4))).id).toBe(id(1));
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${id(2)}/delivery-packages/by-workflow/${id(4)}`,{method:'GET',signal:undefined}]);
 await expect(api.deliveryPackageForWorkflow(id(3),id(8))).rejects.toThrow('DELIVERY_PACKAGE_INVALID');
 await expect(api.deliveryPackageForWorkflow('bad',id(4))).rejects.toThrow();
 expect(request).toHaveBeenCalledTimes(2);
});
it('rediscovers a package from a durable run route with owned project scope',async()=>{
 const request=vi.fn(async()=>resource()),api=new OwnerAPI({request} as never,id(2));
 const pkg=await api.deliveryPackageForRun(id(9));
 expect(pkg.id).toBe(id(1));expect(pkg.project_id).toBe(id(3));
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${id(2)}/delivery-packages/by-run/${id(9)}`,{method:'GET',signal:undefined}]);
 const foreign=resource();foreign.data.tenant_id=id(8);
 await expect(new OwnerAPI({request:vi.fn(async()=>foreign)} as never,id(2)).deliveryPackageForRun(id(9))).rejects.toThrow('DELIVERY_PACKAGE_INVALID');
});

it('requires explicit acceptance of review and refuses terminal or already published packages',()=>{
 const wire=resource();
 const pkg={...readDeliveryPackage(wire,id(2),id(3),id(1)),published_at:null};
 expect(prepareDeliveryPublication(pkg,false).expected_revision).toBe('2');
 pkg.state='review';expect(()=>prepareDeliveryPublication(pkg,false)).toThrow('DELIVERY_REVIEW_CONFIRMATION_REQUIRED');
 expect(prepareDeliveryPublication(pkg,true).id).toBe(id(1));
 for(const state of ['building','failed','cancelled'] as const)expect(()=>prepareDeliveryPublication({...pkg,state},true)).toThrow('DELIVERY_NOT_PUBLISHABLE');
 expect(()=>prepareDeliveryPublication({...pkg,document:null},true)).toThrow();
 expect(()=>prepareDeliveryPublication({...pkg,published_at:'2026-10-10T12:00:00Z'},true)).toThrow();
});


it.each(['😀','я','𐍈'])('accepts exactly 200 Unicode code points in package name %s',character=>{
 const wire=resource();wire.data.name=character.repeat(200);
 expect(readDeliveryPackage(wire,id(2),id(3),id(1)).name).toBe(wire.data.name);
 expect(readPublishedDelivery(wire,id(2),intent()).name).toBe(wire.data.name);
 wire.data.name+=character;
 expect(()=>readDeliveryPackage(wire,id(2),id(3),id(1))).toThrow('DELIVERY_PACKAGE_INVALID');
 expect(()=>readPublishedDelivery(wire,id(2),intent())).toThrow('DELIVERY_PACKAGE_INVALID');
});
