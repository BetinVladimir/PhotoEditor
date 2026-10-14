import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {deliveryDownloadTarget,readDeliveryDownloadIntent,readDeliveryDownloadLink} from './delivery-download';
import type {DeliveryPackage} from './delivery-package';
import type {DeliveryItem} from './delivery-items';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const pkg=():DeliveryPackage=>({id:id(1),tenant_id:id(2),project_id:id(3),revision:'2',workflow_id:id(4),snapshot_id:id(5),name:'Delivery',policy_artifact_id:id(6),manifest_artifact_id:id(7),published_at:'2026-10-10T12:00:00Z',state:'ready',document:{artifact_id:id(7),sha256:'a'.repeat(64)}});
const item=():DeliveryItem=>({id:id(8),ordinal:2,preset_id:id(9),preset_sha:'b'.repeat(64),required:true,export_id:id(10),state:'ready',reason:null});
it('allows only published ready package and ready slot',()=>{
 expect(deliveryDownloadTarget(pkg())).toEqual({kind:'manifest'});
 expect(deliveryDownloadTarget(pkg(),item())).toEqual({kind:'slot',ordinal:2,preset_id:id(9)});
 expect(()=>deliveryDownloadTarget({...pkg(),published_at:null})).toThrow();
 expect(()=>deliveryDownloadTarget({...pkg(),state:'review'})).toThrow();
 expect(()=>deliveryDownloadTarget(pkg(),{...item(),state:'omitted',export_id:null})).toThrow();
 for(const changed of [
  {...item(),id:id(0).replace('4000','0000')},
  {...item(),export_id:'not-an-export'},
  {...item(),preset_sha:'not-a-sha'},
  {...item(),required:'yes' as never},
  {...item(),reason:'stale failure'},
 ])expect(()=>deliveryDownloadTarget(pkg(),changed)).toThrow('DELIVERY_DOWNLOAD_INVALID');
});
it('rejects expired tokens and unsafe or substituted download links',()=>{
 const now=Date.now(),expires_at=new Date(now+45_000).toISOString();
 expect(readDeliveryDownloadIntent({intent_token:'a'.repeat(42)+'A',expires_at},now)).toHaveLength(43);
 for(const intent_token of ['a'.repeat(42),'a'.repeat(44),'a'.repeat(42)+'b'])expect(()=>readDeliveryDownloadIntent({intent_token,expires_at},now)).toThrow();
 expect(()=>readDeliveryDownloadIntent({intent_token:'a'.repeat(42)+'A',expires_at:new Date(now-1).toISOString()},now)).toThrow();
 expect(()=>readDeliveryDownloadIntent({intent_token:'a'.repeat(42)+'A',expires_at:new Date(now+45_000).toUTCString()},now)).toThrow();
 const link={url:'https://objects.example/download?versionId=v1&signature=abc',expires_at,filename:'image.jpg',bytes:42,sha256:'a'.repeat(64)};
 expect(readDeliveryDownloadLink(link,link.sha256,now)).toBe(link.url);
 expect(readDeliveryDownloadLink({...link,url:'http://127.0.0.1:9000/a?versionId=v1'},link.sha256,now)).toBe('http://127.0.0.1:9000/a?versionId=v1');
 for(const url of ['http://objects.example/a?versionId=v1','javascript:alert(1)','https://user:pass@objects.example/a?versionId=v1','https://objects.example/a?versionId=v1#fragment','https://objects.example/a','https://objects.example/a?versionId=v1&versionId=v2','https://objects.example/a?versionId=null'])expect(()=>readDeliveryDownloadLink({...link,url},undefined,now)).toThrow();
 expect(()=>readDeliveryDownloadLink({...link,sha256:'wrong'},undefined,now)).toThrow();
 expect(()=>readDeliveryDownloadLink({...link,sha256:'b'.repeat(64)},link.sha256,now)).toThrow();
 expect(()=>readDeliveryDownloadLink({...link,bytes:536870913},undefined,now)).toThrow();
 expect(()=>readDeliveryDownloadLink({...link,expires_at:new Date(now+4000).toISOString()},undefined,now)).toThrow();
 expect(()=>readDeliveryDownloadLink({...link,expires_at:new Date(now+66_000).toISOString()},undefined,now)).toThrow();
 expect(()=>readDeliveryDownloadLink({...link,expires_at:new Date(now+45_000).toUTCString()},undefined,now)).toThrow();
});
it('creates and redeems a scoped one-use delivery token',async()=>{
 const expires_at=new Date(Date.now()+45_000).toISOString(),request=vi.fn().mockResolvedValueOnce({intent_token:'a'.repeat(42)+'A',expires_at}).mockResolvedValueOnce({url:'https://objects.example/a?versionId=v1',expires_at,filename:'a.jpg',bytes:4,sha256:'b'.repeat(64)});
 const api=new OwnerAPI({request} as never,id(2));
 expect(await api.downloadDelivery(pkg(),item(),'stable-key')).toBe('https://objects.example/a?versionId=v1');
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${id(2)}/delivery-packages/${id(1)}/download-intent`,{method:'POST',body:{kind:'slot',ordinal:2,preset_id:id(9)},idempotencyKey:'stable-key',signal:undefined}]);
 expect(request.mock.calls[1]).toEqual(['/api/v1/downloads/redeem',{method:'POST',body:{token:'a'.repeat(42)+'A'},signal:undefined}]);
});
it('binds a manifest download to the package SHA before exposing its URL',async()=>{
 const expires_at=new Date(Date.now()+45_000).toISOString(),request=vi.fn()
  .mockResolvedValueOnce({intent_token:'a'.repeat(42)+'A',expires_at})
  .mockResolvedValueOnce({url:'https://objects.example/foreign?versionId=v1',expires_at,filename:'manifest.json',bytes:4,sha256:'b'.repeat(64)});
 const api=new OwnerAPI({request} as never,id(2));
 await expect(api.downloadDelivery(pkg(),undefined,'key')).rejects.toThrow('DELIVERY_DOWNLOAD_INVALID');
 expect(request.mock.calls[0][1].body).toEqual({kind:'manifest'});
});
it('refuses a foreign package before IO and an invalid intent before redemption',async()=>{
 const request=vi.fn().mockResolvedValue({intent_token:'bad',expires_at:new Date(Date.now()+45_000).toISOString()});
 const api=new OwnerAPI({request} as never,id(2));
 await expect(api.downloadDelivery({...pkg(),tenant_id:id(99)},undefined,'key')).rejects.toThrow();
 expect(request).not.toHaveBeenCalled();
 await expect(api.downloadDelivery(pkg(),undefined,'key')).rejects.toThrow();
 expect(request).toHaveBeenCalledTimes(1);
});
