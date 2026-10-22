// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {userProfile,tenantSettings} from './test/user-profile';
// Exercise the actual shared boundary with the actual Training screen. These
// fixtures intentionally do not grant authority through a mocked context.
import {useEffect} from 'react';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({request:vi.fn(),post:vi.fn(),push:vi.fn(),replace:vi.fn(),data:new Map<string,string>()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useFocusEffect:(callback:()=>void|(()=>void))=>useEffect(callback,[callback]),useLocalSearchParams:()=>({tenant:'11111111-1111-4111-8111-111111111111',id:'22222222-2222-4222-8222-222222222222'}),router:{push:state.push,replace:state.replace}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./auth-context',()=>{const client={request:state.request},auth={origin:'https://training-boundary.example',client:()=>client};return{useAuth:()=>auth};});
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(key:string)=>state.data.get(key)??null,setItem:async(key:string,value:string)=>{state.data.set(key,value);},removeItem:async(key:string)=>{state.data.delete(key);},getAllKeys:async()=>[...state.data.keys()]}}));
import Training from '../screens/styles/[id]/training';
import {WorkspaceAccessBoundary} from './workspace-access-context';
import {LocaleProvider} from './locale-context';
const tenant='11111111-1111-4111-8111-111111111111',style='22222222-2222-4222-8222-222222222222',project='33333333-3333-4333-8333-333333333333',actor='44444444-4444-4444-8444-444444444444',otherProject='55555555-5555-4555-8555-555555555555';
function view(params:Record<string,unknown>={tenant,id:style}){return <LocaleProvider><WorkspaceAccessBoundary routeName="styles/[id]/training" params={params}><Training/></WorkspaceAccessBoundary></LocaleProvider>}
function prepare(role:string,canEdit=true){state.request.mockImplementation(async(path:string,options?:{method?:string})=>{
 if(options?.method==='POST')return state.post(path,options);
 if(path==='/api/v1/me')return userProfile(actor);
 if(path===`/api/v1/tenants/${tenant}`)return tenantSettings(tenant,role as 'viewer'|'editor'|'admin'|'owner');
 if(path===`/api/v1/tenants/${tenant}/styles/${style}`)return{id:style,tenant_id:tenant,project_id:project,name:'Style',description:'',archived:false,revision:'1',created_at:'2026-10-17T00:00:00Z'};
 if(path===`/api/v1/tenants/${tenant}/projects/${project}`)return{id:project,tenant_id:tenant,can_edit:canEdit};
 if(path===`/api/v1/tenants/${tenant}/projects/${otherProject}`)return{id:otherProject,tenant_id:tenant,can_edit:true};
 throw new Error('unexpected request '+path);
 });}
afterEach(()=>{cleanup();state.data.clear();vi.resetAllMocks();});
it('withholds Training from Viewer before any style, journal or producer IO',async()=>{
 prepare('viewer');render(view());
 await screen.findByRole('alert');
 expect(screen.queryByText('Create training set')).toBeNull();
 expect(state.request.mock.calls.some(([path])=>path.includes('/styles/')||path.includes('/projects/'))).toBe(false);
 expect(state.post).not.toHaveBeenCalled();expect(state.data.size).toBe(0);
});
it('cannot substitute an editable query project for the restricted actual style project',async()=>{
 prepare('editor',false);render(view({tenant,id:style,project:otherProject}));
 const create=await screen.findByText('Create training set');fireEvent.click(create);
 expect(state.request.mock.calls.some(([path])=>path.endsWith(`/projects/${project}`))).toBe(true);
 expect(state.post).not.toHaveBeenCalled();expect(state.data.size).toBe(0);
});
it('withdraws an in-flight training command when the boundary loses tenant scope',async()=>{
 prepare('editor');let complete!:(value:unknown)=>void;
 state.post.mockImplementation(()=>new Promise(resolve=>{complete=resolve}));
 const mounted=render(view());fireEvent.click(await screen.findByText('Create training set'));
 await waitFor(()=>expect(complete).toBeDefined());
 const request=state.post.mock.calls[0][1];
 mounted.rerender(view({tenant:['untrusted'],id:style}));
 expect(request.signal.aborted).toBe(true);
 await act(async()=>{complete({id:otherProject});});
 expect(state.request.mock.calls.some(([path])=>path.includes('/training-sets/'))).toBe(false);
 expect(state.replace).not.toHaveBeenCalled();expect(state.push).not.toHaveBeenCalled();
 expect(screen.queryByText('Training pairs')).toBeNull();
});
