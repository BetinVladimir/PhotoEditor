// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({more:vi.fn(),stops:[] as Array<()=>void>,commit:vi.fn()}));
vi.mock('react-native',async()=>({...await import('react-native-web')}));
vi.mock('./localized-router',()=>({router:{push:vi.fn()},useLocalSearchParams:()=>({tenant:'11111111-1111-4111-8111-111111111111',project:'22222222-2222-4222-8222-222222222222'})}));
const auth={origin:'https://capture-list.test',client:()=>({})};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canEdit:true})}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./capture-session-create',()=>({CaptureSessionCreate:()=>null}));
const data={items:[{id:'first',name:'First',state:'open'}],next_cursor:'next'};
vi.mock('./use-read-resource',()=>({useReadResource:()=>({phase:'ready',data,current:()=>true,commit:state.commit})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{shootSessions=(...args:any[])=>state.more(...args)}}));
import CaptureSessions from '../screens/capture/index';
afterEach(()=>{cleanup();state.more.mockReset();state.commit.mockReset();state.stops=[]});
it('list paging Stop retains current rows and an old Stop cannot abort a replacement page',async()=>{
 const calls:any[][]=[];let finish!:(value:any)=>void;state.more.mockImplementation((...args:any[])=>{calls.push(args);return new Promise(resolve=>finish=resolve)});render(<CaptureSessions/>);fireEvent.click(screen.getByText('capture_more_sessions'));await waitFor(()=>expect(calls).toHaveLength(1));const oldStop=state.stops.at(-1)!;act(()=>oldStop());expect(calls[0][2].aborted).toBe(true);expect(screen.getByText('First')).toBeTruthy();fireEvent.click(screen.getByText('capture_more_sessions'));await waitFor(()=>expect(calls).toHaveLength(2));act(()=>oldStop());expect(calls[1][2].aborted).toBe(false);await act(async()=>finish({items:[{id:'44444444-4444-4444-8444-444444444444',tenant_id:'11111111-1111-4111-8111-111111111111',project_id:'22222222-2222-4222-8222-222222222222',revision:'1',name:'Second',state:'open'}],next_cursor:null}));expect(state.commit).toHaveBeenCalledWith(expect.objectContaining({items:[data.items[0],{id:'44444444-4444-4444-8444-444444444444',tenant_id:'11111111-1111-4111-8111-111111111111',project_id:'22222222-2222-4222-8222-222222222222',revision:'1',name:'Second',state:'open'}]}));
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);const {children,onPress,disabled}=props;if(label==='read_cancel')state.stops.push(onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
