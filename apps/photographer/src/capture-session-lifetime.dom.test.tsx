// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({editors:[] as Array<(items:any[])=>void>,stops:[] as Array<()=>void>,save:vi.fn(),listeners:new Set<(value:string)=>void>(),current:'active',canEdit:true}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return state.current;},addEventListener:(_event:string,callback:(value:string)=>void)=>{state.listeners.add(callback);return{remove:()=>state.listeners.delete(callback)}}}}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:'session',tenant:'tenant'})}));
const auth={origin:'https://capture-lifetime.test',client:()=>({})};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canEdit:state.canEdit})}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./navigation-entry-links',()=>({SessionAnalysisNavigation:()=>null}));
vi.mock('./use-commands',()=>({useCommands:()=>{}}));
vi.mock('./capture-shot-editor',()=>({CaptureShotEditor:({items,onChange}:any)=>{state.editors.push(onChange);return <div data-testid="draft">{items.map((item:any)=>item.label).join(',')}</div>}}));
vi.mock('./capture-shot-load',()=>({loadCaptureShots:async()=>({blocked:false,list:{session_id:'session',revision:'1',requirements:[{id:'shot',label:'Original',person_ids:[],min_confirmed_frames:1,min_quality:.7}]}})}));
vi.mock('./capture-draft-store',()=>({CaptureDraftStore:class{async read(){return null}async write(){}async clear(){}},clearCaptureSessionDrafts:async()=>{}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./owner-api',()=>({OwnerAPI:class{
 shootSession=async()=>({id:'session',project_id:'project',revision:'1',name:'Studio',state:'open',policy:{},capture_policy:null});
 projectAccess=async()=>({can_edit:true});capturePeople=async()=>({items:[],next_cursor:null});captureAlerts=async()=>({items:[],next_cursor:null});get=async()=>({id:'actor'});
 replaceCaptureShotList=(...args:any[])=>state.save(...args);
 streamEvents=(_cursor:any,_event:any,signal:AbortSignal)=>new Promise<void>(resolve=>signal.addEventListener('abort',()=>resolve(),{once:true}));
}}));
import CaptureSession from '../screens/capture/[id]';
afterEach(()=>{cleanup();state.editors=[];state.stops=[];state.save.mockReset();state.listeners.clear();state.current='active';state.canEdit=true});
it('blocks retained draft edits during a save and old Stop cannot cancel its replacement exact-key retry',async()=>{
 const calls:any[][]=[];state.save.mockImplementation((...args:any[])=>{calls.push(args);return new Promise(()=>{})});render(<CaptureSession/>);await screen.findByTestId('draft');const edit=state.editors.at(-1)!;act(()=>edit([{id:'shot',label:'Latest',person_ids:[],min_confirmed_frames:1,min_quality:.7}]));fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(calls).toHaveLength(1));const stop=state.stops.at(-1)!;act(()=>edit([{id:'shot',label:'Stale'}]));expect(screen.getByTestId('draft').textContent).toBe('Latest');act(()=>stop());expect(calls[0][4].aborted).toBe(true);
 fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(calls).toHaveLength(2));expect(calls[1][3]).toBe(calls[0][3]);expect(calls[1][2]).toEqual(calls[0][2]);act(()=>stop());expect(calls[1][4].aborted).toBe(false);
});
it('background aborts the save, refuses late output and retains its original retry body/key',async()=>{
 let finish!:(value:any)=>void;const calls:any[][]=[];state.save.mockImplementation((...args:any[])=>{calls.push(args);return new Promise(resolve=>finish=resolve)});render(<CaptureSession/>);await screen.findByTestId('draft');fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(calls).toHaveLength(1));act(()=>{state.current='background';for(const listener of state.listeners)listener('background')});expect(calls[0][4].aborted).toBe(true);await act(async()=>finish({session_id:'session',revision:'2',requirements:[{id:'shot',label:'Late'}]}));expect(screen.getByTestId('draft').textContent).toBe('Original');act(()=>{state.current='active';for(const listener of state.listeners)listener('active')});fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(calls).toHaveLength(2));expect(calls[1][3]).toBe(calls[0][3]);expect(calls[1][2]).toEqual(calls[0][2]);
});

it('role withdrawal aborts an unknown save without discarding its immutable key before regrant',async()=>{
 const calls:any[][]=[];state.save.mockImplementation((...args:any[])=>{calls.push(args);return new Promise(()=>{})});const view=render(<CaptureSession/>);await screen.findByTestId('draft');fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(calls).toHaveLength(1));state.canEdit=false;view.rerender(<CaptureSession/>);await waitFor(()=>expect(calls[0][4].aborted).toBe(true));state.canEdit=true;view.rerender(<CaptureSession/>);await waitFor(()=>expect((screen.getByText('capture_save').closest('button') as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(calls).toHaveLength(2));expect(calls[1][3]).toBe(calls[0][3]);expect(calls[1][2]).toEqual(calls[0][2]);
});

it('an unknown save keeps its visible draft locked until the same frozen request recovers',async()=>{
 state.save.mockRejectedValue(new TypeError('lost response'));render(<CaptureSession/>);await screen.findByTestId('draft');fireEvent.click(screen.getByText('capture_save'));await screen.findByText('capture_save_error');const edit=state.editors.at(-1)!;act(()=>edit([{id:'shot',label:'Unsubmitted edit'}]));expect(screen.getByTestId('draft').textContent).toBe('Original');fireEvent.click(screen.getByText('capture_save'));await waitFor(()=>expect(state.save).toHaveBeenCalledTimes(2));expect(state.save.mock.calls[1][2]).toEqual(state.save.mock.calls[0][2]);expect(state.save.mock.calls[1][3]).toBe(state.save.mock.calls[0][3]);
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);const {children,onPress,disabled}=props;if(label==='read_cancel')state.stops.push(onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
