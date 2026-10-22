// @vitest-environment jsdom
import {userProfile} from './test/user-profile';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
const navigation=vi.hoisted(()=>({push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:navigation.push}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {WorkspaceAccessContext} from './workspace-access-context';
import {OwnerAPI} from './owner-api';
import {SavedVersionNavigation} from './saved-version-navigation';
const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222',version='33333333-3333-4333-8333-333333333333',asset='44444444-4444-4444-8444-444444444444',actor='55555555-5555-4555-8555-555555555555';
function view(api:OwnerAPI,selected=version,canEdit=true){return <WorkspaceAccessContext.Provider value={{tenant,actor,role:canEdit?'editor':'viewer',canEdit,canAdmin:false}}><SavedVersionNavigation api={api} tenant={tenant} project={project} version={selected}/></WorkspaceAccessContext.Provider>}
function fixture(){const request=vi.fn(async(path:string):Promise<unknown>=>path==='/api/v1/me'?userProfile(actor):{tenant_id:tenant,project_id:project,asset_id:asset,asset_version_id:version});return {request,api:new OwnerAPI({request} as unknown as ApiClient,tenant)}}
afterEach(()=>{cleanup();vi.clearAllMocks();});
it('opens an old accepted receipt via exact readonly lookup without repeating Apply',async()=>{
 const {request,api}=fixture();render(view(api));fireEvent.click(screen.getByText('Open saved version in editor'));
 await waitFor(()=>expect(navigation.push).toHaveBeenCalledWith({pathname:'/editor/[version]',params:{tenant,project,asset,version}}));
 expect(request.mock.calls.map(([path])=>path)).toEqual(['/api/v1/me',`/api/v1/tenants/${tenant}/projects/${project}/versions/${version}/navigation`,'/api/v1/me']);
});
it('ignores an uncooperative old-version response after selected version changes',async()=>{
 const {request,api}=fixture();let finish!:(value:unknown)=>void;
 request.mockImplementation(async(path:string)=>path==='/api/v1/me'?userProfile(actor):await new Promise<unknown>(resolve=>{finish=resolve}) as never);
 const mounted=render(view(api));fireEvent.click(screen.getByText('Open saved version in editor'));await waitFor(()=>expect(finish).toBeDefined());
 mounted.rerender(view(api,asset));await act(async()=>{finish({tenant_id:tenant,project_id:project,asset_id:asset,asset_version_id:version});});
 expect(navigation.push).not.toHaveBeenCalled();
});
it('withdraws opening immediately on role loss and does not navigate after late transport',async()=>{
 const {request,api}=fixture();let finish!:(value:unknown)=>void;
 request.mockImplementation(async(path:string)=>path==='/api/v1/me'?userProfile(actor):await new Promise<unknown>(resolve=>{finish=resolve}) as never);
 const mounted=render(view(api));fireEvent.click(screen.getByText('Open saved version in editor'));await waitFor(()=>expect(finish).toBeDefined());
 mounted.rerender(view(api,version,false));await act(async()=>{finish({tenant_id:tenant,project_id:project,asset_id:asset,asset_version_id:version});});
 expect(navigation.push).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Open saved version in editor'));expect(request).toHaveBeenCalledTimes(2);
});
it('cancel opening keeps the accepted version available for a fresh readonly retry',async()=>{
 const {request,api}=fixture();let finish!:(value:unknown)=>void;
 request.mockImplementationOnce(async()=>userProfile(actor)).mockImplementationOnce(async()=>await new Promise<unknown>(resolve=>{finish=resolve}) as never);
 render(view(api));fireEvent.click(screen.getByText('Open saved version in editor'));await waitFor(()=>expect(finish).toBeDefined());
 fireEvent.click(screen.getByText('Cancel opening'));await act(async()=>{finish({tenant_id:tenant,project_id:project,asset_id:asset,asset_version_id:version});});expect(navigation.push).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Open saved version in editor'));await waitFor(()=>expect(navigation.push).toHaveBeenCalledOnce());
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
