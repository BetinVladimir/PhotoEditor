// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,act} from '@testing-library/react';
import {userProfile} from './test/user-profile';
import {APIError} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
vi.mock('react-native',async()=>import('react-native-web'));
const storage=vi.hoisted(()=>({values:new Map<string,string>()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{
 getItem:async(key:string)=>storage.values.get(key)??null,
 setItem:async(key:string,value:string)=>{storage.values.set(key,value)},
 removeItem:async(key:string)=>{storage.values.delete(key)},getAllKeys:async()=>[...storage.values.keys()],
}}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:'33333333-3333-4333-8333-333333333333',tenant:'11111111-1111-4111-8111-111111111111'})}));
const request=vi.hoisted(()=>vi.fn());
const auth={origin:'https://capture.test',client:()=>({request,eventStream:(_path:string,_cursor:unknown,_event:unknown,signal:AbortSignal)=>new Promise<void>(resolve=>{signal.addEventListener('abort',()=>resolve(),{once:true})})})};
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canEdit:true,canAdmin:false})}));
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
import CaptureSession from '../screens/capture/[id]';
import {LocaleProvider} from './locale-context';
afterEach(()=>{cleanup();storage.values.clear();request.mockReset()});
const shot=(label:string)=>({id:'12345678-1234-1234-1234-123456789abc',label,person_ids:[],min_confirmed_frames:1,min_quality:.7});
describe('Rendered Capture draft/CAS flow',()=>{
 it('preserves edits through locale change and refresh, then requires explicit conflict resolution',async()=>{
  let revision='1';let label='Shared';let conflict=true;
  const writes:Array<{body:{expected_revision:string;requirements:Array<{label:string}>};idempotencyKey:string}>=[];
  request.mockImplementation(async(path:string,options?:{method?:string;body:unknown;idempotencyKey:string})=>{
   if(path.endsWith('/projects/22222222-2222-4222-8222-222222222222'))return{id:'22222222-2222-4222-8222-222222222222',tenant_id:'11111111-1111-4111-8111-111111111111',can_edit:true};
   if(path.endsWith('/me'))return userProfile('55555555-5555-4555-8555-555555555555');
   if(path.includes('/persons?'))return {items:[],next_cursor:null};
   if(path.endsWith('/alerts'))return {items:[],next_cursor:null};
   if(path.endsWith('/shoot-sessions/33333333-3333-4333-8333-333333333333'))return {id:'33333333-3333-4333-8333-333333333333',revision:'1',document:null,data:{tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T00:00:00Z',updated_at:'2026-10-17T00:00:00Z',consent_epoch:'1',resource_type:'shoot_sessions',id:'33333333-3333-4333-8333-333333333333',revision:'1',project_id:'22222222-2222-4222-8222-222222222222',collection_id:'44444444-4444-4444-8444-444444444444',name:'Studio',state:'open',policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:'66666666-6666-4666-8666-666666666666',sha256:'a'.repeat(64)}},capture_policy:null}};
   if(path.endsWith('/shot-list')){
    if(options?.method==='PUT'){
     writes.push(options as typeof writes[number]);
     if(conflict){conflict=false;revision='2';label='Shared changed';throw new APIError(409,'REVISION_CONFLICT')}
     revision='3';label=(options.body as typeof writes[number]['body']).requirements[0].label;
    }
    return {session_id:'33333333-3333-4333-8333-333333333333',revision,document:{artifact_id:'77777777-7777-4777-8777-777777777777',sha256:'b'.repeat(64)},requirements:[shot(label)]};
   }
   throw new Error('Unexpected API route '+path);
  });
  render(<LocaleProvider><CaptureSession/></LocaleProvider>);
  const input=await screen.findByLabelText(en.capture_label);
  fireEvent.change(input,{target:{value:'My draft'}});
  const initialReads=request.mock.calls.length;
  fireEvent.click(screen.getByRole('radio',{name:'Русский'}));
  expect((screen.getByLabelText(ru.capture_label) as HTMLInputElement).value).toBe('My draft');
  expect(request.mock.calls).toHaveLength(initialReads);
  fireEvent.click(screen.getByRole('radio',{name:'English'}));
  fireEvent.click(screen.getByRole('button',{name:en.capture_refresh}));
  await waitFor(()=>expect(request.mock.calls.filter(([path])=>path.endsWith('/shoot-sessions/33333333-3333-4333-8333-333333333333'))).toHaveLength(2));
  expect((screen.getByLabelText(en.capture_label) as HTMLInputElement).value).toBe('My draft');
  await waitFor(()=>expectEnabled(screen.getByRole('button',{name:en.capture_save})));
  await clickWhenEnabled(screen.getByRole('button',{name:en.capture_save}));
  await screen.findByText(en.capture_conflict);
  expect((screen.getAllByLabelText(en.capture_label)[0] as HTMLInputElement).value).toBe('My draft');
  expect((screen.getAllByLabelText(en.capture_label)[1] as HTMLInputElement).value).toBe('Shared changed');
  expect(screen.getByRole('button',{name:en.capture_save}).getAttribute('aria-disabled')).toBe('true');
  fireEvent.click(screen.getByRole('button',{name:en.capture_keep_draft}));
  expect(writes).toHaveLength(1);
  fireEvent.click(screen.getByRole('button',{name:en.capture_save}));
  await waitFor(()=>expect(writes).toHaveLength(2));
  expect(writes[0].body.expected_revision).toBe('1');expect(writes[1].body.expected_revision).toBe('2');
  expect(writes[1].body.requirements[0].label).toBe('My draft');expect(writes[1].idempotencyKey).not.toBe(writes[0].idempotencyKey);
 });
 it('restores a device draft after remount and compares a changed server revision before saving',async()=>{
  let revision='1';let label='Shared';
  request.mockImplementation(async(path:string)=>{
   if(path.endsWith('/projects/22222222-2222-4222-8222-222222222222'))return{id:'22222222-2222-4222-8222-222222222222',tenant_id:'11111111-1111-4111-8111-111111111111',can_edit:true};
   if(path.endsWith('/me'))return userProfile('55555555-5555-4555-8555-555555555555');
   if(path.includes('/persons?')||path.endsWith('/alerts'))return {items:[],next_cursor:null};
   if(path.endsWith('/shoot-sessions/33333333-3333-4333-8333-333333333333'))return {id:'33333333-3333-4333-8333-333333333333',revision:'1',document:null,data:{tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T00:00:00Z',updated_at:'2026-10-17T00:00:00Z',consent_epoch:'1',resource_type:'shoot_sessions',id:'33333333-3333-4333-8333-333333333333',revision:'1',project_id:'22222222-2222-4222-8222-222222222222',collection_id:'44444444-4444-4444-8444-444444444444',name:'Studio',state:'open',policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:'66666666-6666-4666-8666-666666666666',sha256:'a'.repeat(64)}},capture_policy:null}};
   if(path.endsWith('/shot-list'))return {session_id:'33333333-3333-4333-8333-333333333333',revision,document:{artifact_id:'77777777-7777-4777-8777-777777777777',sha256:'b'.repeat(64)},requirements:[shot(label)]};
   throw new Error('Unexpected route');
  });
  const first=render(<LocaleProvider><CaptureSession/></LocaleProvider>);
  fireEvent.change(await screen.findByLabelText(en.capture_label),{target:{value:'Restart draft'}});
  await waitFor(()=>expect([...storage.values.entries()].some(([key,value])=>key.startsWith('photo.capture-draft.v1:')&&JSON.parse(value).requirements[0].label==='Restart draft')).toBe(true));
  first.unmount();revision='2';label='Shared after restart';
  render(<LocaleProvider><CaptureSession/></LocaleProvider>);
  await screen.findByText(en.capture_conflict);
  const fields=screen.getAllByLabelText(en.capture_label) as HTMLInputElement[];
  expect(fields.map(field=>field.value)).toEqual(['Restart draft','Shared after restart']);
  expect(screen.getByRole('button',{name:en.capture_save}).getAttribute('aria-disabled')).toBe('true');
  expect(request.mock.calls.some(([,options])=>options?.method==='PUT')).toBe(false);
 });

});

it('cancels an uncertain shot save, ignores late success and retries its frozen key/body',async()=>{
 let finish!:(value:unknown)=>void;const writes:Array<{body:{expected_revision:string;requirements:Array<{label:string}>};idempotencyKey:string;signal:AbortSignal}>=[];
 request.mockImplementation(async(path:string,options?:any)=>{
  if(path.endsWith('/me'))return userProfile('55555555-5555-4555-8555-555555555555');
  if(path.endsWith('/projects/22222222-2222-4222-8222-222222222222'))return{id:'22222222-2222-4222-8222-222222222222',tenant_id:'11111111-1111-4111-8111-111111111111',can_edit:true};
  if(path.includes('/persons?')||path.endsWith('/alerts'))return{items:[],next_cursor:null};
  if(path.endsWith('/shoot-sessions/33333333-3333-4333-8333-333333333333'))return{id:'33333333-3333-4333-8333-333333333333',revision:'1',document:null,data:{tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T00:00:00Z',updated_at:'2026-10-17T00:00:00Z',consent_epoch:'1',resource_type:'shoot_sessions',id:'33333333-3333-4333-8333-333333333333',revision:'1',project_id:'22222222-2222-4222-8222-222222222222',collection_id:'44444444-4444-4444-8444-444444444444',name:'Studio',state:'open',policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:'66666666-6666-4666-8666-666666666666',sha256:'a'.repeat(64)}},capture_policy:null}};
  if(path.endsWith('/shot-list')){if(options?.method==='PUT'){writes.push(options);if(writes.length===1)return new Promise(resolve=>{finish=resolve});return{session_id:'33333333-3333-4333-8333-333333333333',revision:'2',document:{artifact_id:'77777777-7777-4777-8777-777777777777',sha256:'b'.repeat(64)},requirements:[shot(options.body.requirements[0].label)]};}return{session_id:'33333333-3333-4333-8333-333333333333',revision:'1',document:{artifact_id:'77777777-7777-4777-8777-777777777777',sha256:'b'.repeat(64)},requirements:[shot('Shared')]};}
  throw new Error('Unexpected route '+path);
 });
 render(<LocaleProvider><CaptureSession/></LocaleProvider>);fireEvent.change(await screen.findByLabelText(en.capture_label),{target:{value:'Frozen first'}});fireEvent.click(screen.getByText(en.capture_save));
 await waitFor(()=>expect(writes).toHaveLength(1));fireEvent.click(screen.getByText(en.read_cancel));expect(writes[0].signal.aborted).toBe(true);
 await act(async()=>{finish({session_id:'33333333-3333-4333-8333-333333333333',revision:'2',document:{artifact_id:'77777777-7777-4777-8777-777777777777',sha256:'b'.repeat(64)},requirements:[shot('Late success')]})});expect(screen.queryByDisplayValue('Late success')).toBeNull();
 fireEvent.change(screen.getByLabelText(en.capture_label),{target:{value:'Unsent change'}});fireEvent.click(screen.getByText(en.capture_save));await waitFor(()=>expect(writes).toHaveLength(2));expect(writes[1].body).toEqual(writes[0].body);expect(writes[1].idempotencyKey).toBe(writes[0].idempotencyKey);
});
