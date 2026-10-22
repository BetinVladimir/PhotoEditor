// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {PaperProvider} from 'react-native-paper';
const fixture=vi.hoisted(()=>({request:vi.fn(),push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:'11111111-1111-4111-8111-111111111111',id:'22222222-2222-4222-8222-222222222222'}),router:{push:fixture.push}}));
vi.mock('./auth-context',()=>{const client={request:fixture.request};return {useAuth:()=>({origin:'https://keyboard.test',client:()=>client})}});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'44444444-4444-4444-8444-444444444444',canEdit:true})}));
vi.mock('./navigation-entry-links',()=>({ProfileNavigation:()=>null,WorkspaceSettingsNavigation:()=>null}));
vi.mock('./catalog-create-controls',()=>({CatalogCreateControls:()=>null}));
vi.mock('./collection-create-wizard',()=>({CollectionCreateWizard:()=>null}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{}}}));
vi.mock('./locale-context',async()=>{const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({t:(key:string)=>key})}});
import Workspaces from '../screens/workspaces';
import Projects from '../screens/projects/index';
import ProjectDetail from '../screens/projects/[id]/index';
const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222',collection='33333333-3333-4333-8333-333333333333';
afterEach(()=>{cleanup();fixture.request.mockReset();fixture.push.mockReset()});
it.each(['workspace','project','collection'] as const)('keyboard activates the actual %s row button',async kind=>{
 const workspace={id:tenant,name:'Keyboard studio',role:'owner',revision:'1'};
 const projectRow={id:project,tenant_id:tenant,name:'Keyboard project',description:'',visibility:'workspace',download_allowed:true,revision:'1',created_at:'2026-10-21T00:00:00Z',can_edit:true};
 const collectionRow={id:collection,tenant_id:tenant,project_id:project,name:'Keyboard collection',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-21T00:00:00Z'};
 fixture.request.mockImplementation(async(path:string)=>path==='/api/v1/me'?{id:collection,email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'}:path.includes('/collections?')?{items:[collectionRow],next_cursor:null}:path.includes('/projects/')?projectRow:path.includes('/projects?')?{items:[projectRow],next_cursor:null}:{items:[workspace],next_cursor:null});
 render(<PaperProvider>{kind==='workspace'?<Workspaces/>:kind==='project'?<Projects/>:<ProjectDetail/>}</PaperProvider>);
 const name=kind==='workspace'?'Keyboard studio':kind==='project'?'Keyboard project':'Keyboard collection';
 await screen.findByText(name);
 const button=screen.getByRole('button',{name:new RegExp(name)});
 expect(button.tagName).toBe('BUTTON');
 button.focus();expect(document.activeElement).toBe(button);
 fireEvent.keyDown(button,{key:'Enter',code:'Enter'});fireEvent.keyUp(button,{key:'Enter',code:'Enter'});
 // jsdom does not synthesize the browser's default Enter-to-click activation.
 // Dispatch that native-button activation on the exact focused semantic target.
 fireEvent.click(button,{detail:0});
 expect(fixture.push).toHaveBeenCalledTimes(1);
 expect(fixture.push).toHaveBeenCalledWith(kind==='workspace'?{pathname:'/projects',params:{tenant}}:kind==='project'?{pathname:'/projects/[id]',params:{id:project,tenant}}:{pathname:'/collections/[id]',params:{id:collection,tenant}});
});
