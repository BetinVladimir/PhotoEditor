// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {userProfile,tenantSettings} from './test/user-profile';
import {useEffect} from 'react';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({request:vi.fn(),push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:state.push},useFocusEffect:(callback:()=>void|(()=>void))=>useEffect(callback,[callback])}));
vi.mock('./auth-context',()=>{const client={request:state.request},auth={origin:'https://direct-entry.example',client:()=>client};return{useAuth:()=>auth};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {WorkspaceAccessBoundary} from './workspace-access-context';
import {LocaleProvider} from './locale-context';
const tenant='11111111-1111-4111-8111-111111111111',actor='22222222-2222-4222-8222-222222222222';
function view(routeName='settings/quotas',params:Record<string,unknown>={tenant}){return <LocaleProvider><WorkspaceAccessBoundary routeName={routeName} params={params}><div>direct entry body</div></WorkspaceAccessBoundary></LocaleProvider>}
afterEach(()=>{cleanup();vi.resetAllMocks();});
it('mounts visible returns for a direct tenant Viewer entry only after authority resolves',async()=>{
 let settings!:(value:unknown)=>void;
 state.request.mockImplementation((path:string)=>path==='/api/v1/me'?Promise.resolve(userProfile(actor)):new Promise(resolve=>{settings=resolve}));
 render(view());expect(screen.queryByText('Workspace projects')).toBeNull();expect(screen.queryByText('direct entry body')).toBeNull();
 await waitFor(()=>expect(settings).toBeDefined());await act(async()=>{settings(tenantSettings(tenant,'viewer'));});
 await screen.findByText('direct entry body');fireEvent.click(screen.getByText('Workspace projects'));
 expect(state.push).toHaveBeenCalledWith({pathname:'/projects',params:{tenant}});
});
it('global Profile return does not borrow an untrusted tenant query',()=>{
 render(view('settings/profile',{tenant:['untrusted']}));fireEvent.click(screen.getByText('Workspaces'));
 expect(state.push).toHaveBeenCalledWith('/workspaces');expect(screen.queryByText('Workspace projects')).toBeNull();expect(state.request).not.toHaveBeenCalled();
});
