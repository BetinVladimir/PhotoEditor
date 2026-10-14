import {expect,it,vi} from 'vitest';
import {readDeliveryPage} from './delivery-list';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const item=(n:number,project=id(3))=>({id:id(n),project_id:project,workflow_id:id(n+100),name:`Package ${n}`,state:'ready',revision:'1',created_at:'2026-10-13T12:00:00Z'});
it('binds every page to project and unique package IDs',()=>{
 expect(readDeliveryPage({items:[item(1)],next_cursor:'next'},id(3)).items[0].id).toBe(id(1));
 expect(()=>readDeliveryPage({items:[item(1,id(9))],next_cursor:null},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[item(1)],next_cursor:null},id(3),new Set([id(1)]))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[{...item(1),revision:'1x'}],next_cursor:null},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[{...item(1),revision:'9007199254740992'}],next_cursor:null},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[{...item(1),created_at:'2026-02-30T12:00:00Z'}],next_cursor:null},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[{...item(1),name:'x'.repeat(201)}],next_cursor:null},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[],next_cursor:'next'},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[],next_cursor:null,secret:'extra'},id(3))).toThrow('DELIVERY_LIST_INVALID');
});
it('requires ascending keyset order within and across pages at nanosecond precision',()=>{
 const first={...item(1),created_at:'2026-10-13T12:00:00.123000001Z'};
 const later={...item(2),created_at:'2026-10-13T12:00:00.123000002Z'};
 expect(readDeliveryPage({items:[first,later],next_cursor:null},id(3)).items).toHaveLength(2);
 expect(()=>readDeliveryPage({items:[later,first],next_cursor:null},id(3))).toThrow('DELIVERY_LIST_INVALID');
 expect(()=>readDeliveryPage({items:[first],next_cursor:null},id(3),new Set(),later)).toThrow('DELIVERY_LIST_INVALID');
 expect(readDeliveryPage({items:[later],next_cursor:null},id(3),new Set(),first).items).toHaveLength(1);
});
it('requests a project-scoped cursor page and refuses invalid scope before IO',async()=>{
 const request=vi.fn(async()=>({items:[item(1)],next_cursor:null}));
 const api=new OwnerAPI({request} as never,id(2));
 expect((await api.deliveryPackages(id(3),'cursor')).items).toHaveLength(1);
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${id(2)}/delivery-packages?project_id=${id(3)}&limit=50&cursor=cursor`,{method:'GET',signal:undefined}]);
 await expect(api.deliveryPackages('bad')).rejects.toThrow();expect(request).toHaveBeenCalledTimes(1);
});
