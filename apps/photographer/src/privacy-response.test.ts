import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
import {privacyUpdateBody,readPrivacyPreferences,readDeletionPreflight} from './privacy-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const prefs=()=>({revision:'9223372036854775807',analytics:false,marketing:true,updated_at:'2026-11-01T12:00:00Z'});
const preflight=()=>({privacy_case_id:id(1),revision:'1',ownership_choices:[{tenant_id:id(2),tenant_revision:'9223372036854775807',allowed_actions:['close','transfer']}],required_recent_auth_purpose:'delete_profile'});
it('preserves exact revisions and rejects unexpected private fields or malformed preferences',()=>{
 expect(readPrivacyPreferences(prefs()).revision).toBe('9223372036854775807');
 for(const changed of [{revision:'9223372036854775808'},{analytics:'false'},{email:'private@example.invalid'},{updated_at:'yesterday'}])expect(()=>readPrivacyPreferences({...prefs(),...changed})).toThrow();
 for(const value of [{},{analytics:undefined},{analytics:null},{analytics:true,extra:1}])expect(()=>privacyUpdateBody(value as any)).toThrow();
});
it('owns preflight arrays and rejects duplicate or foreign extra details',()=>{
 const input=preflight(),result=readDeletionPreflight(input);input.ownership_choices[0].allowed_actions.length=0;expect(result.ownership_choices[0].allowed_actions).toEqual(['close','transfer']);
 const invalid=preflight();invalid.ownership_choices.push({...invalid.ownership_choices[0]});expect(()=>readDeletionPreflight(invalid)).toThrow();
 expect(()=>readDeletionPreflight({...preflight(),owner_email:'private'})).toThrow();
 expect(()=>readDeletionPreflight({...preflight(),required_recent_auth_purpose:'login'})).toThrow();
});
it('uses real user-global privacy routes and sends CAS/key outside the body',async()=>{
 const request=vi.fn().mockResolvedValue({...prefs(),revision:'2'}),api=new OwnerAPI({request} as unknown as ApiClient,'');
 const body={analytics:false};await api.updatePrivacyPreferences(body,'1','saved-key');
 expect(request.mock.calls[0][0]).toBe('/api/v1/me/privacy');expect(request.mock.calls[0][1]).toMatchObject({method:'PATCH',body:{analytics:false},idempotencyKey:'saved-key',headers:{'If-Match':'"1"'}});
 request.mockResolvedValue(preflight());expect((await api.prepareProfileDeletion('preflight-key')).privacy_case_id).toBe(id(1));
 expect(request.mock.calls[1][0]).toBe('/api/v1/me/deletion/preflight');expect(request.mock.calls[1][1]).toMatchObject({method:'POST',body:{},idempotencyKey:'preflight-key'});
});
it('rejects invalid input and cancellation before network; rejects inconsistent server preferences',async()=>{
 const request=vi.fn().mockResolvedValue({...prefs(),revision:'2',analytics:true}),api=new OwnerAPI({request} as unknown as ApiClient,'');
 await expect(api.updatePrivacyPreferences({analytics:false},'1','key')).rejects.toThrow();
 request.mockClear();await expect(api.updatePrivacyPreferences({analytics:false},'0','key')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 const controller=new AbortController();controller.abort();await expect(api.prepareProfileDeletion('key',controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
