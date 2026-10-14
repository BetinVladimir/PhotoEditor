// @vitest-environment jsdom
import React from 'react';
import {AppState,type AppStateStatus} from 'react-native';
import {render,screen,waitFor,fireEvent,cleanup,act} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticPreview} from './semantic-preview';
const id='11111111-1111-4111-8111-111111111111';
const output={artifact_id:id,role:'derivative',sha256:'a'.repeat(64),bytes:123,media_type:'image/webp',url:'https://objects.example/preview',expires_at:new Date(Date.now()+60000).toISOString()};
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.useRealTimers();});
it('loads accepted image and clears it when access fails',async()=>{
 const runOutputs=vi.fn().mockResolvedValueOnce({items:[output]}).mockRejectedValueOnce(new Error('DENIED'));
 render(<SemanticPreview client={{runOutputs}} tenant={id} run={id}/>);
 fireEvent.click(screen.getByText('View preview'));
 await waitFor(()=>expect(screen.getByLabelText('Edited photo preview')).toBeTruthy());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Reload preview'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Reload preview'}));
 await waitFor(()=>expect(screen.getByText(/Unable to load preview/)).toBeTruthy());
 expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
});
it('aborts request on unmount',async()=>{
 const runOutputs=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 const mounted=render(<SemanticPreview client={{runOutputs}} tenant={id} run={id}/>);
 fireEvent.click(screen.getByText('View preview'));await waitFor(()=>expect(runOutputs).toHaveBeenCalledOnce());
 const signal=runOutputs.mock.calls[0][2] as AbortSignal;mounted.unmount();expect(signal.aborted).toBe(true);
});

it('hides image when its signed link expires',async()=>{
 vi.useFakeTimers({toFake:['setTimeout','clearTimeout']});
 try{
  const runOutputs=vi.fn().mockResolvedValue({items:[{...output,expires_at:new Date(Date.now()+1000).toISOString()}]});
  render(<SemanticPreview client={{runOutputs}} tenant={id} run={id}/>);
  await act(async()=>{fireEvent.click(screen.getByText('View preview'))});
  expect(screen.getByLabelText('Edited photo preview')).toBeTruthy();
  await act(async()=>{vi.advanceTimersByTime(1001)});
  expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
  expect(screen.getByText(/Preview link expired/)).toBeTruthy();
 }finally{cleanup();vi.useRealTimers()}
});

it('hides a previous signed preview immediately when authenticated client changes',async()=>{const first={runOutputs:vi.fn().mockResolvedValue({items:[output]})},next={runOutputs:vi.fn().mockRejectedValue(new Error('DENIED'))};const view=render(<SemanticPreview client={first} tenant={id} run={id}/>);fireEvent.click(screen.getByText('View preview'));await screen.findByLabelText('Edited photo preview');view.rerender(<SemanticPreview client={next} tenant={id} run={id}/>);expect(screen.queryByLabelText('Edited photo preview')).toBeNull();fireEvent.click(screen.getByText('View preview'));await screen.findByRole('alert');expect(screen.queryByLabelText('Edited photo preview')).toBeNull();});

it('drops signed image in background and requires a fresh authorized preview read',async()=>{
 let change!:(state:AppStateStatus)=>void;const remove=vi.fn();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{change=listener;return {remove};});
 const client={runOutputs:vi.fn().mockResolvedValueOnce({items:[output]}).mockRejectedValueOnce(new Error('DENIED'))};
 const view=render(<SemanticPreview client={client} tenant={id} run={id}/>);await act(async()=>fireEvent.click(screen.getByText('View preview')));
 expect(screen.getByLabelText('Edited photo preview')).toBeTruthy();act(()=>change('background'));
 expect(screen.queryByLabelText('Edited photo preview')).toBeNull();expect(screen.getByRole('button',{name:'View preview'}).hasAttribute('disabled')).toBe(true);
 await act(async()=>change('active'));expect(client.runOutputs).toHaveBeenCalledTimes(1);
 await act(async()=>fireEvent.click(screen.getByText('View preview')));expect(client.runOutputs).toHaveBeenCalledTimes(2);expect(screen.getByRole('alert')).toBeTruthy();expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
 view.unmount();expect(remove).toHaveBeenCalledOnce();
});
it('aborts a backgrounded preview and ignores late signed output',async()=>{
 let change!:(state:AppStateStatus)=>void,finish!:(value:{items:typeof output[]})=>void;vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{change=listener;return {remove:vi.fn()};});
 const client={runOutputs:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))};render(<SemanticPreview client={client} tenant={id} run={id}/>);
 fireEvent.click(screen.getByText('View preview'));act(()=>change('inactive'));expect((client.runOutputs.mock.calls[0][2] as AbortSignal).aborted).toBe(true);
 await act(async()=>{change('active');finish({items:[output]});});expect(screen.queryByLabelText('Edited photo preview')).toBeNull();expect(client.runOutputs).toHaveBeenCalledTimes(1);
});
