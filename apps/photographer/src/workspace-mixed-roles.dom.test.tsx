// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {userProfile,tenantSettings} from './test/user-profile';
import type {ReactNode} from 'react';
import {afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,act} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const fixture=vi.hoisted(()=>({request:vi.fn(),push:vi.fn(),tenant:'11111111-1111-4111-8111-111111111111'}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:fixture.tenant}),router:{push:fixture.push}}));
vi.mock('./auth-context',()=>{const client={request:fixture.request};const auth={origin:'https://role.test',client:()=>client};return{useAuth:()=>auth}});
import Members from '../screens/settings/members';
import Retention from '../screens/settings/retention';
import Projects from '../screens/projects/index';
import {LocaleProvider} from './locale-context';
import {WorkspaceAccessContext,type WorkspaceAccess} from './workspace-access-context';
const actor='22222222-2222-4222-8222-222222222222';
function mount(node:ReactNode,role:WorkspaceAccess['role']='viewer'){return render(<LocaleProvider><WorkspaceAccessContext.Provider value={{tenant:fixture.tenant,actor,role,canEdit:role!=='viewer',canAdmin:role==='admin'||role==='owner'}}>{node}</WorkspaceAccessContext.Provider></LocaleProvider>)}
afterEach(()=>{cleanup();fixture.request.mockReset();fixture.push.mockReset()});
describe('mixed read/edit screens honor current membership',()=>{
 it.each(['viewer','editor'] as const)('%s may read members but cannot invite/suspend',async role=>{
  fixture.request.mockImplementation(async(path:string)=>path==='/api/v1/me'?userProfile(actor):{items:[{id:actor,user_id:actor,email:'person@example.test',role:'editor',status:'active',revision:'1'}],next_cursor:null});
  mount(<Members/>,role);await screen.findByText('person@example.test');fireEvent.change(screen.getByPlaceholderText('Email'),{target:{value:'new@example.test'}});fireEvent.click(screen.getByText('Invite editor'));fireEvent.click(screen.getByText('person@example.test'));
  expect(fixture.request.mock.calls.every(([,options])=>!options?.method||options.method==='GET')).toBe(true);
 });
 it('Admin may invite with the current tenant path',async()=>{
  fixture.request.mockImplementation(async(path:string,options?:{method?:string})=>path==='/api/v1/me'?userProfile(actor):options?.method==='POST'?{id:actor,email:'new@example.test',role:'editor',expires_at:'2027-01-01T00:00:00Z'}:{items:[],next_cursor:null});
  mount(<Members/>,'admin');await screen.findByText('No members');fireEvent.change(screen.getByPlaceholderText('Email'),{target:{value:'new@example.test'}});fireEvent.click(screen.getByText('Invite editor'));
  await waitFor(()=>expect(fixture.request.mock.calls.some(([path,options])=>path===`/api/v1/tenants/${fixture.tenant}/invites`&&options.method==='POST')).toBe(true));
 });
 it('Viewer sees retention values and cannot submit a patch',async()=>{
  fixture.request.mockResolvedValue(tenantSettings(fixture.tenant,'viewer'));
  mount(<Retention/>);await screen.findByDisplayValue('30');fireEvent.click(screen.getByText('Save retention'));expect(fixture.request.mock.calls.every(([,options])=>!options?.method||options.method==='GET')).toBe(true);
 });
 it('Viewer project browsing stays useful while creation is disabled',async()=>{
  fixture.request.mockResolvedValue({items:[{id:actor,tenant_id:fixture.tenant,name:'Studio',description:'',visibility:'workspace',download_allowed:true,revision:'1',created_at:'2026-10-17T00:00:00Z',can_edit:false}],next_cursor:null});mount(<Projects/>);await screen.findByText('Studio');fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'New'}});fireEvent.click(screen.getByRole('button',{name:'Create'}));expect(fixture.request.mock.calls.every(([,options])=>!options?.method||options.method==='GET')).toBe(true);fireEvent.click(screen.getByText('Studio'));expect(fixture.push).toHaveBeenCalledWith({pathname:'/projects/[id]',params:{id:actor,tenant:fixture.tenant}});
 });
});

it('keeps an uncertain invitation frozen after cancellation and ignores late completion',async()=>{
 let complete!:(value:unknown)=>void;const posted:any[]=[];
 fixture.request.mockImplementation(async(_path:string,options?:any)=>{if(_path==='/api/v1/me')return userProfile(actor);if(options?.method==='POST'){posted.push(options);if(posted.length===1)return new Promise(resolve=>{complete=resolve});return{};}return{items:[],next_cursor:null};});
 mount(<Members/>,'admin');await screen.findByText('No members');fireEvent.change(screen.getByPlaceholderText('Email'),{target:{value:'frozen@example.test'}});fireEvent.click(screen.getByText('Invite editor'));await waitFor(()=>expect(posted).toHaveLength(1));
 fireEvent.click(screen.getByText('Cancel member change request'));await screen.findByText('Retry saved member change');expect(posted[0].signal.aborted).toBe(true);
 await act(async()=>{complete({})});expect(screen.getByDisplayValue('frozen@example.test')).toBeTruthy();
 fireEvent.click(screen.getByText('Retry saved member change'));await waitFor(()=>expect(posted).toHaveLength(2));expect(posted[1].body).toEqual(posted[0].body);expect(posted[1].idempotencyKey).toBe(posted[0].idempotencyKey);
});
