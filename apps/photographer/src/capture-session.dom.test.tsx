// @vitest-environment jsdom
import {afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
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
vi.mock('expo-router',()=>({useLocalSearchParams:()=>({id:'session',tenant:'tenant'})}));
const request=vi.hoisted(()=>vi.fn());
const auth={origin:'https://capture.test',client:()=>({request,eventStream:(_path:string,_cursor:unknown,_event:unknown,signal:AbortSignal)=>new Promise<void>(resolve=>{signal.addEventListener('abort',()=>resolve(),{once:true})})})};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
import CaptureSession from '../app/capture/[id]';
import {LocaleProvider} from './locale-context';
afterEach(()=>{cleanup();storage.values.clear();request.mockReset()});
const shot=(label:string)=>({id:'12345678-1234-1234-1234-123456789abc',label,person_ids:[],min_confirmed_frames:1,min_quality:.7});
describe('Rendered Capture draft/CAS flow',()=>{
 it('preserves edits through locale change and refresh, then requires explicit conflict resolution',async()=>{
  let revision='1';let label='Shared';let conflict=true;
  const writes:Array<{body:{expected_revision:string;requirements:Array<{label:string}>};idempotencyKey:string}>=[];
  request.mockImplementation(async(path:string,options?:{method?:string;body:unknown;idempotencyKey:string})=>{
   if(path.endsWith('/me'))return {id:'user'};
   if(path.includes('/persons?'))return {items:[],next_cursor:null};
   if(path.endsWith('/alerts'))return {items:[],next_cursor:null};
   if(path.endsWith('/shoot-sessions/session'))return {id:'session',revision:'1',document:null,data:{resource_type:'shoot_sessions',id:'session',revision:'1',project_id:'project',collection_id:'collection',name:'Studio',state:'open',policy:{},capture_policy:null}};
   if(path.endsWith('/shot-list')){
    if(options?.method==='PUT'){
     writes.push(options as typeof writes[number]);
     if(conflict){conflict=false;revision='2';label='Shared changed';throw new APIError(409,'REVISION_CONFLICT')}
     revision='3';label=(options.body as typeof writes[number]['body']).requirements[0].label;
    }
    return {session_id:'session',revision,document:null,requirements:[shot(label)]};
   }
   throw new Error('Unexpected API route '+path);
  });
  render(<LocaleProvider><CaptureSession/></LocaleProvider>);
  const input=await screen.findByLabelText(en.capture_label);
  fireEvent.change(input,{target:{value:'My draft'}});
  const initialReads=request.mock.calls.length;
  fireEvent.click(screen.getByRole('button',{name:'Русский'}));
  expect((screen.getByLabelText(ru.capture_label) as HTMLInputElement).value).toBe('My draft');
  expect(request.mock.calls).toHaveLength(initialReads);
  fireEvent.click(screen.getByRole('button',{name:'English'}));
  fireEvent.click(screen.getByRole('button',{name:en.capture_refresh}));
  await waitFor(()=>expect(request.mock.calls.filter(([path])=>path.endsWith('/shoot-sessions/session'))).toHaveLength(2));
  expect((screen.getByLabelText(en.capture_label) as HTMLInputElement).value).toBe('My draft');
  await waitFor(()=>expect(screen.getByRole('button',{name:en.capture_save}).getAttribute('aria-disabled')).not.toBe('true'));
  fireEvent.click(screen.getByRole('button',{name:en.capture_save}));
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
   if(path.endsWith('/me'))return {id:'user'};
   if(path.includes('/persons?')||path.endsWith('/alerts'))return {items:[],next_cursor:null};
   if(path.endsWith('/shoot-sessions/session'))return {id:'session',revision:'1',document:null,data:{resource_type:'shoot_sessions',id:'session',revision:'1',project_id:'project',collection_id:'collection',name:'Studio',state:'open',policy:{},capture_policy:null}};
   if(path.endsWith('/shot-list'))return {session_id:'session',revision,document:null,requirements:[shot(label)]};
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
