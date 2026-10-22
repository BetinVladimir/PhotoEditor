// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {userProfile,tenantSettings} from './test/user-profile';
import {useEffect} from 'react';
import {afterEach,describe,it,expect,vi} from 'vitest';
import {act,cleanup,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const state=vi.hoisted(()=>({request:vi.fn(),origin:'https://scope.test'}));
vi.mock('./localized-router',()=>({useFocusEffect:(callback:()=>void|(()=>void))=>useEffect(callback,[callback])}));
vi.mock('./auth-context',()=>{const client={request:state.request};const auth={origin:state.origin,client:()=>client};return {useAuth:()=>auth}});
import {LocaleProvider} from './locale-context';
import {WorkspaceAccessBoundary,useWorkspaceAccess} from './workspace-access-context';
const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222',actor='33333333-3333-4333-8333-333333333333';
function Child(){const access=useWorkspaceAccess(tenant);return <div data-testid="content">{access?.canEdit?'editable':'readonly'}</div>}
function view(routeName='projects/index',params:Record<string,unknown>={tenant}){return <LocaleProvider><WorkspaceAccessBoundary routeName={routeName} params={params}><Child/></WorkspaceAccessBoundary></LocaleProvider>}
function responses(role='viewer',can_edit=true){state.request.mockImplementation(async(path:string)=>{if(path==='/api/v1/me')return userProfile(actor);if(path.endsWith(`/assets/${actor}`))return{id:actor,tenant_id:tenant,project_id:project};if(path.endsWith(`/projects/${project}`))return{id:project,tenant_id:tenant,can_edit};return tenantSettings(tenant,role as 'viewer'|'editor'|'admin'|'owner')})}
afterEach(()=>{cleanup();state.request.mockReset()});
describe('mounted workspace authorization',()=>{
 it.each([
  ['runs/[id]',{tenant}],
  ['editor/[version]',{tenant,asset:actor}],
  ['multi-replacements/[original]',{tenant,project}],
  ['collections/[id]/lens',{tenant,project}],
 ])('refuses missing path identity before authority transport: %s',(route,params)=>{
  render(view(route,params));
  expect(state.request).not.toHaveBeenCalled();
  expect(screen.queryByTestId('content')).toBeNull();
 });

 it('never mounts tenant children during pending authority',async()=>{
  let complete!:(v:unknown)=>void;state.request.mockImplementation((path:string)=>path==='/api/v1/me'?Promise.resolve(userProfile(actor)):new Promise(resolve=>{complete=resolve}));
  render(view());expect(screen.queryByTestId('content')).toBeNull();
  await waitFor(()=>expect(complete).toBeDefined());complete(tenantSettings(tenant,'viewer'));
  expect(await screen.findByTestId('content')).toHaveProperty('textContent','readonly');
 });
 it('refuses bad tenant before any transport but permits global profile',()=>{
  const rendered=render(view('projects/index',{tenant:['invalid']}));expect(state.request).not.toHaveBeenCalled();expect(screen.queryByTestId('content')).toBeNull();
  rendered.rerender(view('settings/profile',{tenant:['invalid']}));expect(screen.getByTestId('content')).toHaveProperty('textContent','readonly');expect(state.request).not.toHaveBeenCalled();
 });
 it('retains viewer browsing and denies the editor workbench',async()=>{
  responses();const rendered=render(view());await screen.findByTestId('content');rendered.rerender(view('editor/[version]',{tenant,version:project,asset:actor}));
  await waitFor(()=>expect(state.request.mock.calls.length).toBeGreaterThan(2));expect(screen.queryByTestId('content')).toBeNull();
 });
 it('does not let a tenant editor override restricted project access',async()=>{
  responses('editor',false);render(view('collections/[id]/lens',{tenant,id:actor,project}));
  await waitFor(()=>expect(state.request.mock.calls.some(([path])=>path.endsWith(`/projects/${project}`))).toBe(true));expect(screen.queryByTestId('content')).toBeNull();
 });
 it('resolves the actual editor asset project even without a project query',async()=>{
  responses('editor',false);render(view('editor/[version]',{tenant,asset:actor,version:project}));
  await waitFor(()=>expect(state.request.mock.calls.some(([path])=>path.endsWith(`/projects/${project}`))).toBe(true));expect(screen.queryByTestId('content')).toBeNull();
 });
 it('rejects a supplied project that differs from the actual asset without reading its authority',async()=>{
  responses('editor');
  render(view('editor/[version]',{tenant,asset:actor,version:project,project:actor}));
  await waitFor(()=>expect(state.request.mock.calls.some(([path])=>path.endsWith(`/assets/${actor}`))).toBe(true));
  await act(async()=>{});
  expect(state.request.mock.calls.some(([path])=>path.includes('/projects/'))).toBe(false);
  expect(screen.queryByTestId('content')).toBeNull();
 });
 it('does not adopt old authority when a cancelled transport resolves after a tenant switch',async()=>{
  let oldSettings!:(v:unknown)=>void;
  state.request.mockImplementation((path:string)=>{
   if(path==='/api/v1/me')return Promise.resolve(userProfile(actor));
   if(path===`/api/v1/tenants/${tenant}`)return new Promise(resolve=>{oldSettings=resolve});
   return new Promise(()=>{});
  });
  const rendered=render(view());
  await waitFor(()=>expect(oldSettings).toBeDefined());
  rendered.rerender(view('projects/index',{tenant:project}));
  await act(async()=>{oldSettings({id:tenant,role:'owner',revision:'1'});});
  expect(screen.queryByTestId('content')).toBeNull();
 });
 it('withdraws the prior tenant immediately when route scope changes',async()=>{
  responses('editor');const rendered=render(view());expect(await screen.findByTestId('content')).toHaveProperty('textContent','editable');
  state.request.mockImplementation(()=>new Promise(()=>{}));rendered.rerender(view('projects/index',{tenant:project}));expect(screen.queryByTestId('content')).toBeNull();
 });
});
