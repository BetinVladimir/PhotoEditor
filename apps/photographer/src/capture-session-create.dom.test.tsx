// @vitest-environment jsdom
import {afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import type {ApiClient} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
import {CaptureSessionCreate} from './capture-session-create';
import {OwnerAPI} from './owner-api';
afterEach(cleanup);
describe('Capture creation rendered web flow',()=>{
 it('retains admission body/key after a lost response and reads policies from the V2 envelope',async()=>{
  const policy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:'calibration',sha256:'a'.repeat(64)}};
  const capture_policy={schema_version:1,checks:['focus','clipping'],max_queue_age_seconds:30,calibration:policy.calibration};
  const resource=(id:string,name:string)=>({id,revision:'1',document:null,data:{id,revision:'1',resource_type:'shoot_sessions',project_id:'project',collection_id:'collection',name,state:'open',policy,capture_policy}});
  let lost=true;const mutations:Array<{body:unknown;idempotencyKey:string}>=[];
  const request=vi.fn(async(path:string,options?:{method?:string;body?:unknown;idempotencyKey?:string})=>{
   if(path.includes('/collections?'))return {items:[{id:'collection',name:'Frames'}],next_cursor:null};
   if(path.endsWith('/shoot-sessions/template'))return resource('template','Existing studio');
   if(options?.method==='POST'){
    mutations.push({body:options.body,idempotencyKey:options.idempotencyKey!});
    if(lost){lost=false;throw new TypeError('response lost after server commit')}
    return resource('created','New studio');
   }
   throw new Error('Unexpected route');
  });
  const onCreated=vi.fn(),onBusy=vi.fn();
  render(<CaptureSessionCreate api={new OwnerAPI({request} as unknown as ApiClient,'tenant')} project="project" sessions={[{id:'template',name:'Existing studio'}]} onCreated={onCreated} onBusy={onBusy}/>);
  fireEvent.click(await screen.findByRole('radio',{name:'Frames'}));
  fireEvent.click(screen.getByRole('radio',{name:'Existing studio'}));
  fireEvent.change(screen.getByLabelText(en.capture_session_name),{target:{value:'New studio'}});
  fireEvent.click(screen.getByRole('button',{name:en.capture_create_session}));
  await screen.findByText(en.capture_create_error);
  expect(onCreated).not.toHaveBeenCalled();
  await waitFor(()=>expect(screen.getByRole('button',{name:en.capture_create_session}).getAttribute('aria-disabled')).not.toBe('true'));
  fireEvent.click(screen.getByRole('button',{name:en.capture_create_session}));
  await waitFor(()=>expect(onCreated).toHaveBeenCalledTimes(1));
  expect(mutations).toHaveLength(2);expect(mutations[1]).toEqual(mutations[0]);
  expect(mutations[0].body).toEqual({project_id:'project',collection_id:'collection',name:'New studio',policy,capture_policy});
  expect(request.mock.calls.filter(([path])=>path.endsWith('/shoot-sessions/template'))).toHaveLength(1);
  expect(onCreated.mock.calls[0][0]).toEqual(resource('created','New studio').data);
  expect(onBusy.mock.calls.map(([value])=>value)).toEqual([true,false,true,false]);
 });
 it('refuses a template with no Capture policy before submitting a session',async()=>{
  const request=vi.fn(async(path:string)=>path.includes('/collections?')?{items:[{id:'collection',name:'Frames'}],next_cursor:null}:{id:'template',revision:'1',document:null,data:{id:'template',revision:'1',resource_type:'shoot_sessions',project_id:'project',capture_policy:null}});
  const onCreated=vi.fn();
  render(<CaptureSessionCreate api={new OwnerAPI({request} as unknown as ApiClient,'tenant')} project="project" sessions={[{id:'template',name:'Ordinary session'}]} onCreated={onCreated} onBusy={()=>{}}/>);
  fireEvent.click(await screen.findByRole('radio',{name:'Frames'}));
  fireEvent.click(screen.getByRole('radio',{name:'Ordinary session'}));
  expect(screen.getByRole('radio',{name:/Frames/}).getAttribute('aria-checked')).toBe('true');
  fireEvent.change(screen.getByLabelText(en.capture_session_name),{target:{value:'New studio'}});
  fireEvent.click(screen.getByRole('button',{name:en.capture_create_session}));
  await screen.findByText(en.capture_template_unavailable);
  expect(onCreated).not.toHaveBeenCalled();expect(request).toHaveBeenCalledTimes(2);
 });

});
