import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {readUserProfile} from './profile-response';
const row=()=>({id:'00000000-0000-4000-8000-000000000001',email:'photo@example.test',locale:'en' as const,theme:'system' as const,timezone:'UTC',revision:'9223372036854775806'});
it('decodes the real User projection and preserves exact int64 revision',()=>{expect(readUserProfile(row())).toEqual(row());for(const value of [{...row(),revision:'+1'},{...row(),revision:'01'},{...row(),revision:'9223372036854775808'},{...row(),theme:'unknown'},{...row(),locale:null},{...row(),timezone:'Local'},{...row(),extra:true}])expect(()=>readUserProfile(value)).toThrow();});
it('mounts decoder and binds update to captured identity, revision and preferences',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise<unknown>(r=>{resolve=r})),api=new OwnerAPI({request} as never,'');
 const before=row(),patch={timezone:'Europe/Sofia'},pending=api.updateMe(before,patch);before.revision='1';patch.timezone='Asia/Tokyo';resolve({...row(),revision:'9223372036854775807',timezone:'Europe/Sofia'});expect((await pending).timezone).toBe('Europe/Sofia');expect(request.mock.calls[0]).toMatchObject(['/api/v1/me',{method:'PATCH',headers:{'If-Match':'"9223372036854775806"'},body:{timezone:'Europe/Sofia'}}]);
 for(const bad of [{...row(),timezone:'Europe/Sofia'},{...row(),revision:'9223372036854775807'}, {...row(),id:'00000000-0000-4000-8000-000000000002',revision:'9223372036854775807',timezone:'Europe/Sofia'},{...row(),email:'other@example.test',revision:'9223372036854775807',timezone:'Europe/Sofia'}]){const next=api.updateMe(row(),{timezone:'Europe/Sofia'});resolve(bad);await expect(next).rejects.toThrow('RECEIPT');}
 const read=api.me();resolve({});await expect(read).rejects.toThrow('PROFILE_RESPONSE');
});
it('refuses cancelled IO and late response, and malformed input before transport',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise<unknown>(r=>{resolve=r})),api=new OwnerAPI({request} as never,'');const abort=new AbortController();
 const pending=api.me(abort.signal);abort.abort();resolve(row());await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(api.me(abort.signal)).rejects.toMatchObject({name:'AbortError'});await expect(api.updateMe(row(),{},abort.signal)).rejects.toMatchObject({name:'AbortError'});await expect(api.updateMe({...row(),revision:'+1'},{})).rejects.toThrow();expect(request).toHaveBeenCalledTimes(1);
});
