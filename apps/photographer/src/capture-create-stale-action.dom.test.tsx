// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({rows:new Map<string,string>(),create:[] as Array<()=>void>,names:[] as Array<(v:string)=>void>,stops:[] as Array<()=>void>}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.rows.get(k)??null,setItem:async(k:string,v:string)=>{state.rows.set(k,v);},removeItem:async(k:string)=>{state.rows.delete(k);},getAllKeys:async()=>[...state.rows.keys()]}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_o:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('react-native',async()=>({...await import('react-native-web'),
 
 }));
import {CaptureSessionCreate} from './capture-session-create';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const calibration={artifact_id:id(5),sha256:'a'.repeat(64)},policy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration},capture_policy={schema_version:1,checks:['focus'],max_queue_age_seconds:30,calibration};
function api(){return {tenant:id(2),me:vi.fn().mockResolvedValue({id:id(1)}),get:vi.fn().mockResolvedValue({items:[{id:id(4),tenant_id:id(2),project_id:id(3),name:'Frames',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'}],next_cursor:null}),shootSession:vi.fn().mockResolvedValue({id:id(6),project_id:id(3),policy,capture_policy}),captureCapabilities:vi.fn().mockResolvedValue({available:true}),createShootSession:vi.fn().mockRejectedValue(new TypeError('lost response'))};}
afterEach(()=>{cleanup();state.rows.clear();state.create=[];state.names=[];state.stops=[];});
it('retained Create reads the latest draft and old input handlers cannot edit an unknown attempt',async()=>{
 const service=api();render(<CaptureSessionCreate api={service as any} origin="https://capture-stale.test" project={id(3)} sessions={[{id:id(6),name:'Template'}]} onCreated={()=>{}} onBusy={()=>{}}/>);
 await screen.findByRole('radio',{name:'Frames'});const retained=state.create[0],edit=state.names[0];
 expect(screen.getByRole('radio',{name:'Frames'}).getAttribute('aria-checked')).toBe('false');fireEvent.click(screen.getByRole('radio',{name:'Frames'}));expect(screen.getByRole('radio',{name:/Frames/}).getAttribute('aria-checked')).toBe('true');fireEvent.click(screen.getByRole('radio',{name:'Template'}));expect(screen.getByRole('radio',{name:/Template/}).getAttribute('aria-checked')).toBe('true');fireEvent.change(screen.getByLabelText('capture_session_name'),{target:{value:'Latest'}});
 await act(async()=>{retained();});await screen.findByText('capture_create_error');
 expect(service.createShootSession).toHaveBeenCalledWith(expect.objectContaining({name:'Latest',collection_id:id(4)}),expect.any(String),expect.any(AbortSignal));
 act(()=>edit('Stale edit'));expect((screen.getByLabelText('capture_session_name') as HTMLInputElement).value).toBe('Latest');
});
it('replacing the API remounts the scope and rejects retained creation and input handlers',async()=>{
 const first=api(),second=api(),props={origin:'https://capture-stale.test',project:id(3),sessions:[{id:id(6),name:'Template'}],onCreated:()=>{},onBusy:()=>{}};
 const view=render(<CaptureSessionCreate {...props} api={first as any}/>);await screen.findByRole('radio',{name:'Frames'});
 fireEvent.click(screen.getByRole('radio',{name:'Frames'}));fireEvent.click(screen.getByRole('radio',{name:'Template'}));fireEvent.change(screen.getByLabelText('capture_session_name'),{target:{value:'Old'}});
 const retained=state.create.at(-1)!,edit=state.names.at(-1)!;view.rerender(<CaptureSessionCreate {...props} api={second as any}/>);
 await waitFor(()=>expect(second.get).toHaveBeenCalled());act(()=>{retained();edit('Leaked');});
 expect((screen.getByLabelText('capture_session_name') as HTMLInputElement).value).toBe('');expect(first.shootSession).not.toHaveBeenCalled();expect(second.shootSession).not.toHaveBeenCalled();
});

it('a retained Stop cannot abort a replacement saved-request flight',async()=>{
 const service=api();const signals:AbortSignal[]=[];service.createShootSession.mockImplementation((_body:any,_key:any,signal:AbortSignal)=>{signals.push(signal);return new Promise(()=>{});});
 render(<CaptureSessionCreate api={service as any} origin="https://capture-stale.test" project={id(3)} sessions={[{id:id(6),name:'Template'}]} onCreated={()=>{}} onBusy={()=>{}}/>);
 fireEvent.click(await screen.findByRole('radio',{name:'Frames'}));fireEvent.click(screen.getByRole('radio',{name:'Template'}));fireEvent.change(screen.getByLabelText('capture_session_name'),{target:{value:'Current'}});fireEvent.click(screen.getByRole('button',{name:'capture_create_session'}));await waitFor(()=>expect(signals).toHaveLength(1));const oldStop=state.stops.at(-1)!;act(()=>oldStop());expect(signals[0].aborted).toBe(true);
 fireEvent.click(screen.getByRole('button',{name:'capture_create_session'}));await waitFor(()=>expect(signals).toHaveLength(2));act(()=>oldStop());expect(signals[1].aborted).toBe(false);expect(screen.getByRole('button',{name:'read_cancel'})).toBeTruthy();fireEvent.click(screen.getByRole('button',{name:'read_cancel'}));expect(signals[1].aborted).toBe(true);
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);const {children,onPress,disabled}=props;if(label==='capture_create_session')state.create.push(onPress);
if(label==='read_cancel')state.stops.push(onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)},TextInput:(props:any)=>{state.names.push(props.onChangeText);return React.createElement(paper.TextInput,props)}};});
