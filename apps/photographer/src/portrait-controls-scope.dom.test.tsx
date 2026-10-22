// @vitest-environment jsdom
import {expectEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const state=vi.hoisted(()=>({reads:[] as string[],submit:vi.fn(),rejectVersion:''}));
vi.mock('./local-portrait-attempt-store',()=>({LocalPortraitAttemptStore:class {
 constructor(_storage:unknown,_origin:string,_user:string,_tenant:string,_project:string,private version:string){}
 async read(){state.reads.push(this.version);if(this.version===state.rejectVersion)throw new Error('SOURCE_CHANGED');return {body:{snapshot_id:'unused'}};}
}}));
vi.mock('./open-eyes-attempt-store',()=>({OpenEyesAttemptStore:class {
 constructor(_storage:unknown,_origin:string,_user:string,_tenant:string,_project:string,private version:string){}
 async read(){state.reads.push(this.version);if(this.version===state.rejectVersion)throw new Error('SOURCE_CHANGED');return {body:{snapshot_id:'unused'}};}
}}));
vi.mock('./local-portrait-attempt',()=>({LocalPortraitAttempt:{restore:()=>({state:'accepted',submit:state.submit})}}));
vi.mock('./open-eyes-attempt',()=>({OpenEyesAttempt:{restore:()=>({state:'accepted',submit:state.submit})}}));
vi.mock('./reflection-attempt-store',()=>({ReflectionAttemptStore:class {
 constructor(_storage:unknown,_origin:string,_user:string,_tenant:string,_project:string,private version:string){}
 async read(){state.reads.push(this.version);if(this.version===state.rejectVersion)throw new Error('SOURCE_CHANGED');return {body:{snapshot_id:'unused'}};}
}}));
vi.mock('./reflection-attempt',()=>({ReflectionAttempt:{restore:()=>({state:'accepted',submit:state.submit})}}));
vi.mock('./glare-donor-attempt-store',()=>({GlareDonorAttemptStore:class {
 constructor(_storage:unknown,_origin:string,_user:string,_tenant:string,_project:string,private version:string){}
 async read(){state.reads.push(this.version);if(this.version===state.rejectVersion)throw new Error('SOURCE_CHANGED');return {body:{snapshot_id:'unused'}};}
}}));
vi.mock('./glare-donor-attempt',()=>({GlareDonorAttempt:{restore:()=>({state:'accepted',submit:state.submit})}}));
import {ReflectionControls} from './reflection-controls';
import {GlareDonorControls} from './glare-donor-controls';
import {LocalPortraitControls} from './local-portrait-controls';
import {OpenEyesControls} from './open-eyes-controls';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(()=>{cleanup();state.reads=[];state.rejectVersion='';state.submit.mockReset();});
for(const [name,Control] of [['local portrait',LocalPortraitControls],['open eyes',OpenEyesControls],['reflection',ReflectionControls],['glare donor',GlareDonorControls]] as const){
 it(`${name} drops the previous journal immediately when the base version changes`,async()=>{
  const props={api:{} as OwnerAPI,origin:'https://photo.test',user:id(1),tenant:id(2),project:id(3),baseVersion:id(4),onSubmitted:vi.fn()};
  const view=render(<Control {...props}/>);
  await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Open result'})));
  state.rejectVersion=id(5);
  view.rerender(<Control {...props} baseVersion={id(5)}/>);
  await screen.findByRole('alert');
  expect(state.reads).toEqual([id(4),id(5)]);
  const button=screen.getByRole('button',{name:'Prepare and submit'});
  expect(button.getAttribute('aria-disabled')).toBe('true');fireEvent.click(button);
  expect(state.submit).not.toHaveBeenCalled();expect(props.onSubmitted).not.toHaveBeenCalled();
 });
}
