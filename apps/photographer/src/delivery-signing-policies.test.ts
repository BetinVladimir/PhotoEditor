import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {deliverySigningMIMEs,readDeliverySigningPolicies} from './delivery-signing-policies';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const choice=(n:number)=>({id:id(n),sha256:'a'.repeat(64),revision:'1'});

it('owns a sorted SHA-pinned Delivery policy catalogue and refuses changed fields',()=>{
 const source={items:[choice(1),choice(2)]},decoded=readDeliverySigningPolicies(source);
 source.items[0].sha256='b'.repeat(64);
 expect(decoded[0].sha256).toBe('a'.repeat(64));
 for(const invalid of [{items:[choice(2),choice(1)]},{items:[choice(1),choice(1)]},{items:[{...choice(1),sha256:'A'.repeat(64)}]},{items:[{...choice(1),revision:'01'}]},{items:[{...choice(1),key_handle:'secret'}]},{items:[],extra:true},null])expect(()=>readDeliverySigningPolicies(invalid)).toThrow();
});

it('asks for each selected MIME once and rejects unsupported TIFF before HTTP',async()=>{
 const request=vi.fn().mockResolvedValue({items:[choice(1)]});
 const api=new OwnerAPI({request} as unknown as ApiClient,id(2));
 const result=await api.deliverySigningPolicies(id(3),['png','jpeg','png']);
 expect(result).toEqual([choice(1)]);
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(2)}/projects/${id(3)}/delivery-signing-policies?mime=image%2Fjpeg&mime=image%2Fpng`);
 expect(request.mock.calls[0][1]).toMatchObject({method:'GET'});
 expect(deliverySigningMIMEs(['jpeg','png'])).toEqual(['image/jpeg','image/png']);
 await expect(api.deliverySigningPolicies(id(3),['tiff'])).rejects.toThrow();
 expect(request).toHaveBeenCalledTimes(1);
});
