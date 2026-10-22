// @vitest-environment jsdom
import React from 'react';
import {AppState,type AppStateStatus} from 'react-native';
import {render,screen,waitFor,fireEvent,cleanup,act} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticPreview} from './semantic-preview';
import {APIError} from '@photoeditor/api-client';
const id='11111111-1111-4111-8111-111111111111';
const output={artifact_id:id,sha256:'a'.repeat(64),media_type:'image/png' as const,width:100,height:100,url:'https://objects.example/preview',expires_at:new Date(Date.now()+60000).toISOString()};
const reply=(after=output)=>({schema_version:1 as const,tenant_id:id,target_id:id,child_run_id:id,working_image:{artifact_id:id,sha256:'b'.repeat(64)},after});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.useRealTimers();});
it('loads accepted image and clears it when access fails',async()=>{
 const semanticAppliedPreview=vi.fn().mockResolvedValueOnce(reply(output)).mockRejectedValueOnce(new Error('DENIED'));
 render(<SemanticPreview client={{semanticAppliedPreview}} tenant={id} target={id} run={id}/>);
 fireEvent.click(screen.getByText('View preview'));
 await waitFor(()=>expect(screen.getByLabelText('Edited photo preview')).toBeTruthy());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Reload preview'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Reload preview'}));
 await waitFor(()=>expect(screen.getByText(/Unable to load preview/)).toBeTruthy());
 expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
});
it('aborts request on unmount',async()=>{
 const semanticAppliedPreview=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 const mounted=render(<SemanticPreview client={{semanticAppliedPreview}} tenant={id} target={id} run={id}/>);
 fireEvent.click(screen.getByText('View preview'));await waitFor(()=>expect(semanticAppliedPreview).toHaveBeenCalledOnce());
 const signal=semanticAppliedPreview.mock.calls[0][2] as AbortSignal;mounted.unmount();expect(signal.aborted).toBe(true);
});

it('hides image when its signed link expires',async()=>{
 vi.useFakeTimers({toFake:['setTimeout','clearTimeout']});
 try{
  const semanticAppliedPreview=vi.fn().mockResolvedValue(reply({...output,expires_at:new Date(Date.now()+1000).toISOString()}));
  render(<SemanticPreview client={{semanticAppliedPreview}} tenant={id} target={id} run={id}/>);
  await act(async()=>{fireEvent.click(screen.getByText('View preview'))});
  expect(screen.getByLabelText('Edited photo preview')).toBeTruthy();
  await act(async()=>{vi.advanceTimersByTime(1001)});
  expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
  expect(screen.getByText(/Preview link expired/)).toBeTruthy();
 }finally{cleanup();vi.useRealTimers()}
});

it('hides a previous signed preview immediately when authenticated client changes',async()=>{const first={semanticAppliedPreview:vi.fn().mockResolvedValue(reply(output))},next={semanticAppliedPreview:vi.fn().mockRejectedValue(new Error('DENIED'))};const view=render(<SemanticPreview client={first} tenant={id} target={id} run={id}/>);fireEvent.click(screen.getByText('View preview'));await screen.findByLabelText('Edited photo preview');view.rerender(<SemanticPreview client={next} tenant={id} target={id} run={id}/>);expect(screen.queryByLabelText('Edited photo preview')).toBeNull();fireEvent.click(screen.getByText('View preview'));await screen.findByRole('alert');expect(screen.queryByLabelText('Edited photo preview')).toBeNull();});

it('drops signed image in background and requires a fresh authorized preview read',async()=>{
 let change!:(state:AppStateStatus)=>void;const remove=vi.fn();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{change=listener;return {remove};});
 const client={semanticAppliedPreview:vi.fn().mockResolvedValueOnce(reply(output)).mockRejectedValueOnce(new Error('DENIED'))};
 const view=render(<SemanticPreview client={client} tenant={id} target={id} run={id}/>);await act(async()=>fireEvent.click(screen.getByText('View preview')));
 expect(screen.getByLabelText('Edited photo preview')).toBeTruthy();act(()=>change('background'));
 expect(screen.queryByLabelText('Edited photo preview')).toBeNull();expect(screen.getByRole('button',{name:'View preview'}).hasAttribute('disabled')).toBe(true);
 await act(async()=>change('active'));expect(client.semanticAppliedPreview).toHaveBeenCalledTimes(1);
 await act(async()=>fireEvent.click(screen.getByText('View preview')));expect(client.semanticAppliedPreview).toHaveBeenCalledTimes(2);expect(screen.getByRole('alert')).toBeTruthy();expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
 view.unmount();expect(remove).toHaveBeenCalledOnce();
});
it('aborts a backgrounded preview and ignores late signed output',async()=>{
 let change!:(state:AppStateStatus)=>void,finish!:(value:ReturnType<typeof reply>)=>void;vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{change=listener;return {remove:vi.fn()};});
 const client={semanticAppliedPreview:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))};render(<SemanticPreview client={client} tenant={id} target={id} run={id}/>);
 fireEvent.click(screen.getByText('View preview'));act(()=>change('inactive'));expect((client.semanticAppliedPreview.mock.calls[0][2] as AbortSignal).aborted).toBe(true);
 await act(async()=>{change('active');finish(reply(output));});expect(screen.queryByLabelText('Edited photo preview')).toBeNull();expect(client.semanticAppliedPreview).toHaveBeenCalledTimes(1);
});
it('binds each preview to its target even when photographs share a child run',async()=>{
 const second='22222222-2222-4222-8222-222222222222';
 const client={semanticAppliedPreview:vi.fn().mockImplementation(async(_tenant:string,target:string)=>({...reply(),target_id:target,after:{...output,url:'https://objects.example/'+target}}))};
 render(<><SemanticPreview client={client} tenant={id} target={id} run={id}/><SemanticPreview client={client} tenant={id} target={second} run={id}/></>);
 for(const button of screen.getAllByText('View preview'))fireEvent.click(button);
 await waitFor(()=>expect(screen.getAllByLabelText('Edited photo preview')).toHaveLength(2));
 expect(client.semanticAppliedPreview.mock.calls.map(call=>call[1]).sort()).toEqual([id,second]);
});
it('rejects a response belonging to another target or child run',async()=>{
 const client={semanticAppliedPreview:vi.fn().mockResolvedValue({...reply(),target_id:'22222222-2222-4222-8222-222222222222'})};
 render(<SemanticPreview client={client} tenant={id} target={id} run={id}/>);fireEvent.click(screen.getByText('View preview'));
 await screen.findByRole('alert');expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
});

it.each([401,403,404,410])('withdraws parent results after target preview denial %s',async(status)=>{
 const onAccessLost=vi.fn(),client={semanticAppliedPreview:vi.fn().mockRejectedValue(new APIError(status,'ACCESS_DENIED'))};
 render(<SemanticPreview client={client} tenant={id} target={id} run={id} onAccessLost={onAccessLost}/>);fireEvent.click(screen.getByText('View preview'));
 await waitFor(()=>expect(onAccessLost).toHaveBeenCalledOnce());expect(screen.queryByLabelText('Edited photo preview')).toBeNull();
 const button=screen.getByRole('button',{name:'View preview'});expect(button.hasAttribute('disabled')).toBe(true);
 fireEvent.click(button);expect(client.semanticAppliedPreview).toHaveBeenCalledOnce();
});

it('keeps a definitive refusal blocked without a parent withdrawal callback',async()=>{
 const client={semanticAppliedPreview:vi.fn().mockRejectedValue(new APIError(403,'ACCESS_DENIED'))};
 render(<SemanticPreview client={client} tenant={id} target={id} run={id}/>);fireEvent.click(screen.getByText('View preview'));
 await screen.findByRole('alert');const button=screen.getByRole('button',{name:'View preview'});
 expect(button.hasAttribute('disabled')).toBe(true);fireEvent.click(button);expect(client.semanticAppliedPreview).toHaveBeenCalledOnce();
});

it('cancelled applied preview cannot replace a fresh retry image',async()=>{
 let finish!:(value:ReturnType<typeof reply>)=>void;const semanticAppliedPreview=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue(reply({...output,url:'https://objects.example/fresh'}));
 render(<SemanticPreview client={{semanticAppliedPreview}} tenant={id} target={id} run={id}/>);fireEvent.click(screen.getByText('View preview'));await waitFor(()=>expect(semanticAppliedPreview).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('View preview'));await screen.findByLabelText('Edited photo preview');await act(async()=>finish(reply()));
 expect(document.querySelector('img[src="https://objects.example/preview"]')).toBeNull();expect(document.querySelector('img[src="https://objects.example/fresh"]')).toBeTruthy();
});
