import {expect,it,vi} from 'vitest';
import type {OwnerAPI} from './owner-api';
import {versionEditorTarget} from './version-navigation';
const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222',version='33333333-3333-4333-8333-333333333333',asset='44444444-4444-4444-8444-444444444444',actor='55555555-5555-4555-8555-555555555555';
const receipt={tenant_id:tenant,project_id:project,asset_id:asset,asset_version_id:version};
function api(value:unknown=receipt){return {me:vi.fn().mockResolvedValue({id:actor}),get:vi.fn().mockResolvedValue(value)} as unknown as Pick<OwnerAPI,'me'|'get'>;}
it('resolves a historical saved version without substituting latest version or creating another result',async()=>{
 const client=api(),target=await versionEditorTarget(client,tenant,project,version,actor,new AbortController().signal,()=>true);
 expect(target).toEqual({pathname:'/editor/[version]',params:{tenant,project,asset,version}});expect(client.me).toHaveBeenCalledTimes(2);
 expect(client.get).toHaveBeenCalledWith(`/api/v1/tenants/{t}/projects/${project}/versions/${version}/navigation`,expect.any(AbortSignal));
});
it.each([{...receipt,tenant_id:project},{...receipt,project_id:tenant},{...receipt,asset_version_id:asset},{...receipt,asset_id:null},{...receipt,asset_id:'00000000-0000-0000-0000-000000000000'},{...receipt,latest_version_id:asset}])('refuses foreign or malformed projection %#',async value=>{
 await expect(versionEditorTarget(api(value),tenant,project,version,actor,new AbortController().signal,()=>true)).rejects.toThrow('VERSION_NAVIGATION_INVALID');
});
it('refuses pre-cancel without IO and late cancellation after an uncooperative projection',async()=>{
 const client=api(),cancelled=new AbortController();cancelled.abort();await expect(versionEditorTarget(client,tenant,project,version,actor,cancelled.signal,()=>true)).rejects.toThrow();expect(client.me).not.toHaveBeenCalled();
 const controller=new AbortController();vi.mocked(client.get).mockImplementation(async()=>{controller.abort();return receipt as never;});
 await expect(versionEditorTarget(client,tenant,project,version,actor,controller.signal,()=>true)).rejects.toThrow();expect(client.me).toHaveBeenCalledOnce();
});
it('refuses an actor different from mounted authority or changed during lookup',async()=>{
 const changed=api();vi.mocked(changed.me).mockResolvedValue({id:asset} as never);
 await expect(versionEditorTarget(changed,tenant,project,version,actor,new AbortController().signal,()=>true)).rejects.toThrow('VERSION_NAVIGATION_ACTOR_INVALID');expect(changed.get).not.toHaveBeenCalled();
 const late=api();vi.mocked(late.me).mockResolvedValueOnce({id:actor} as never).mockResolvedValueOnce({id:asset} as never);
 await expect(versionEditorTarget(late,tenant,project,version,actor,new AbortController().signal,()=>true)).rejects.toThrow('VERSION_NAVIGATION_ACTOR_CHANGED');
});
