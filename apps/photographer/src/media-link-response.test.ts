import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {readMediaLinks} from './media-link-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({asset_version_id:id(2),artifact_sha256:'a'.repeat(64),icc_sha256:'b'.repeat(64),profile_hash:'c'.repeat(64),variant:'thumb640' as const,url:'https://objects.example/thumb?versionId=v1',expires_at:new Date(Date.now()+30000).toISOString(),width:640,height:400,media_type:'image/jpeg'});
it('requires the exact requested version, variants, verified hashes and bounded versioned URL',()=>{
 const wire=row();expect(readMediaLinks({items:[wire]},id(2),['thumb640']).items).toEqual([wire]);
 for(const bad of [{...row(),asset_version_id:id(3)},{...row(),profile_hash:undefined},{...row(),url:'https://objects.example/master'},{...row(),url:'https://objects.example/thumb?versionId=v1&versionId=v2'},{...row(),width:641},{...row(),expires_at:new Date(Date.now()-1).toISOString()}])expect(()=>readMediaLinks({items:[bad]},id(2),['thumb640'])).toThrow();
 expect(()=>readMediaLinks({items:[row(),row()]},id(2),['thumb640'])).toThrow();
});
it('freezes requested variants and refuses pre/late cancellation at actual transport',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise<unknown>(r=>{resolve=r})),api=new OwnerAPI({request} as never,id(1));const variants:Array<'thumb640'>=['thumb640'];
 const pending=api.mediaLinks(id(3),id(2),variants);variants.length=0;resolve({items:[row()]});expect((await pending).items[0].variant).toBe('thumb640');
 const abort=new AbortController(),late=api.mediaLinks(id(3),id(2),['thumb640'],abort.signal);abort.abort();resolve({items:[row()]});await expect(late).rejects.toMatchObject({name:'AbortError'});await expect(api.mediaLinks(id(3),id(2),['thumb640'],abort.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(2);
});
