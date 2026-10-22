// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
const renderedStops=vi.hoisted(()=>[] as Array<()=>void>);
const data=vi.hoisted(()=>new Map<string,string>());
const removal=vi.hoisted(()=>({pause:null as null|(()=>Promise<void>)}));
const app=vi.hoisted(()=>({state:'active',change:null as null|((state:string)=>void),listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>{
 const native=await import('react-native-web'),React=await import('react');
 return {...native,
  AppState:{get currentState(){return app.state},addEventListener:(_name:string,callback:(state:string)=>void)=>{app.listeners.add(callback);app.change=state=>{app.state=state;for(const listener of [...app.listeners])listener(state)};return{remove:()=>{app.listeners.delete(callback)}}}}
 };
});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{await removal.pause?.();data.delete(k)}}}));
import {PersistentSemanticCreate} from './persistent-semantic-create';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {Collection} from './catalog-types';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const collection={id,name:'Portraits',revision:'9007199254740993'} as Collection,mask={artifact_id:id,sha256:'c'.repeat(64)},binding={asset_version_id:id,source:{artifact_id:other,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},ticket={run_id:id,workflow_id:other,state:'queued'};
beforeEach(()=>{Object.defineProperty(navigator,'locks',{configurable:true,value:{request:async(_name:string,_options:unknown,action:()=>Promise<unknown>)=>action()}})});
afterEach(()=>{cleanup();data.clear();Reflect.deleteProperty(navigator,'locks');app.state='active';app.change=null;app.listeners.clear();renderedStops.length=0;removal.pause=null});
it('recovers unknown admission without the old live mask and reuses exact body/key',async()=>{
 const api={me:vi.fn().mockResolvedValue({id}),semanticRunIntent:vi.fn().mockResolvedValue({run_id:id,workflow_id:other,intent_id:null,revision:'2',state:'queued'}),v2Binding:vi.fn().mockResolvedValue(binding),selectionSnapshot:vi.fn().mockResolvedValue({id:other,digest:'d'.repeat(64),item_count:1,expires_at:'2099-01-01T00:00:00Z'}),proposeSemanticIntent:vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue(ticket)};
 const props={api:api as unknown as OwnerAPI,origin:'https://journal.test',tenant:id,asset:id,version:id,collection,onSubmitted:vi.fn()};
 const view=render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));
 await screen.findByText('Retry batch request');const original=api.proposeSemanticIntent.mock.calls[0].slice(0,2);view.unmount();
 render(<PersistentSemanticCreate {...props}/>);fireEvent.click(await screen.findByText('Prepare batch review'));
 await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.proposeSemanticIntent.mock.calls[1].slice(0,2)).toEqual(original);expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
 cleanup();api.proposeSemanticIntent.mockClear();const acceptedView=render(<PersistentSemanticCreate {...props} disabled/>);fireEvent.click(await screen.findByText('Prepare batch review'));
 await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(2));expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
 acceptedView.rerender(<PersistentSemanticCreate {...props}/>);fireEvent.click(screen.getByText('Start another batch'));await waitFor(()=>expect(data.size).toBe(0));expect(screen.queryByText('Prepare batch review')).toBeNull();
});
it('recovers unknown snapshot with the original key and frozen revision',async()=>{
 const api={me:vi.fn().mockResolvedValue({id}),semanticRunIntent:vi.fn().mockResolvedValue({run_id:id,workflow_id:other,intent_id:null,revision:'2',state:'queued'}),v2Binding:vi.fn().mockResolvedValue(binding),selectionSnapshot:vi.fn().mockRejectedValueOnce(new Error('connection lost')).mockResolvedValue({id:other,digest:'d'.repeat(64),item_count:1,expires_at:'2099-01-01T00:00:00Z'}),proposeSemanticIntent:vi.fn().mockResolvedValue(ticket)};
 const props={api:api as unknown as OwnerAPI,origin:'https://snapshot-journal.test',tenant:id,asset:id,version:id,collection,onSubmitted:vi.fn()};
 const view=render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await screen.findByText('Retry batch request');const original=api.selectionSnapshot.mock.calls[0].slice(0,2);view.unmount();
 render(<PersistentSemanticCreate {...props} collection={{...collection,revision:'9007199254740994'}}/>);fireEvent.click(await screen.findByText('Prepare batch review'));
 await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledWith(ticket));expect(api.selectionSnapshot.mock.calls[1].slice(0,2)).toEqual(original);
});
function recoveryFixture(){
 const api={me:vi.fn().mockResolvedValue({id}),semanticRunIntent:vi.fn().mockResolvedValue({run_id:id,workflow_id:other,intent_id:null,revision:'2',state:'queued'}),v2Binding:vi.fn().mockResolvedValue(binding),selectionSnapshot:vi.fn().mockResolvedValue({id:other,digest:'d'.repeat(64),item_count:1,expires_at:'2099-01-01T00:00:00Z'}),proposeSemanticIntent:vi.fn().mockResolvedValue(ticket)};
 const props={api:api as unknown as OwnerAPI,origin:'https://authority.test',tenant:id,asset:id,version:id,collection,onSubmitted:vi.fn()};return{api,props};
}
it.each([401,403,404])('hides an accepted journal after current parent authority returns %s',async(status)=>{
 const{api,props}=recoveryFixture();const first=render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(1));first.unmount();
 api.semanticRunIntent.mockRejectedValue(new APIError(status,'FORBIDDEN'));api.proposeSemanticIntent.mockClear();
 render(<PersistentSemanticCreate {...props}/>);await screen.findByRole('alert');expect(screen.queryByText('Prepare batch review')).toBeNull();expect(screen.queryByText('Start another batch')).toBeNull();expect(api.proposeSemanticIntent).not.toHaveBeenCalled();expect(data.size).toBe(1);
});
it('rejects substituted parent workflow before displaying saved request fields',async()=>{
 const{api,props}=recoveryFixture();const first=render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(1));first.unmount();
 api.semanticRunIntent.mockResolvedValue({run_id:id,workflow_id:id,intent_id:null,revision:'2',state:'queued'});
 render(<PersistentSemanticCreate {...props}/>);await screen.findByRole('alert');expect(screen.queryByLabelText('Strength')).toBeNull();
});
it('keeps frozen body after current source version advances, without replacing its binding',async()=>{
 const{api,props}=recoveryFixture();api.proposeSemanticIntent.mockRejectedValueOnce(new Error('lost response'));
 const first=render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await screen.findByText('Retry batch request');const original=api.proposeSemanticIntent.mock.calls[0].slice(0,2);first.unmount();
 api.v2Binding.mockResolvedValue({...binding,asset_version_id:other});render(<PersistentSemanticCreate {...props}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(1));expect(api.proposeSemanticIntent.mock.calls[1].slice(0,2)).toEqual(original);
});
it('hides uncertain request after source editor permission is revoked',async()=>{
 const{api,props}=recoveryFixture();api.proposeSemanticIntent.mockRejectedValueOnce(new Error('lost response'));
 const first=render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await screen.findByText('Retry batch request');first.unmount();
 api.v2Binding.mockRejectedValue(new APIError(403,'FORBIDDEN'));api.proposeSemanticIntent.mockClear();render(<PersistentSemanticCreate {...props}/>);await screen.findByRole('alert');expect(screen.queryByText('Prepare batch review')).toBeNull();expect(api.proposeSemanticIntent).not.toHaveBeenCalled();expect(data.size).toBe(1);
});
it('hides restored fields in background and rechecks authority on foreground',async()=>{
 const{api,props}=recoveryFixture();render(<PersistentSemanticCreate {...props} mask={mask}/>);await screen.findByText('Prepare batch review');
 act(()=>app.change?.('background'));expect(screen.queryByText('Prepare batch review')).toBeNull();
 api.v2Binding.mockRejectedValue(new APIError(403,'FORBIDDEN'));act(()=>app.change?.('active'));
 await screen.findByRole('alert');expect(screen.queryByText('Prepare batch review')).toBeNull();expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
});
it('aborts foreground authority flight and ignores a late response while backgrounded',async()=>{
 const{api,props}=recoveryFixture();let resolve!:(v:typeof binding)=>void;
 api.v2Binding.mockImplementationOnce(()=>new Promise(r=>{resolve=r}));render(<PersistentSemanticCreate {...props} mask={mask}/>);
 await waitFor(()=>expect(api.v2Binding).toHaveBeenCalledTimes(1));const signal=api.v2Binding.mock.calls[0][1] as AbortSignal;
 act(()=>app.change?.('background'));expect(signal.aborted).toBe(true);await act(async()=>resolve(binding));expect(screen.queryByText('Prepare batch review')).toBeNull();
 act(()=>app.change?.('active'));await screen.findByText('Prepare batch review');expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
});
it('does not reveal a new form when accepted-receipt clearing completes in background',async()=>{
 const{api,props}=recoveryFixture();render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(1));
 let release!:()=>void;const paused=new Promise<void>(resolve=>{release=resolve});removal.pause=vi.fn(()=>paused);
 fireEvent.click(screen.getByText('Start another batch'));await waitFor(()=>expect(removal.pause).toHaveBeenCalled());act(()=>app.change?.('background'));
 await act(async()=>{release();await paused});expect(screen.queryByText('Prepare batch review')).toBeNull();expect(screen.queryByText('Start another batch')).toBeNull();
});
it('offers Stop/Retry for stalled journal bootstrap without creating a semantic command',async()=>{
 const {api,props}=recoveryFixture();let release!:(v:{id:string})=>void;api.me.mockImplementationOnce(()=>new Promise(done=>{release=done}));render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry loading'));await screen.findByText('Prepare batch review');await act(async()=>release({id}));expect(api.proposeSemanticIntent).not.toHaveBeenCalled();expect(api.selectionSnapshot).not.toHaveBeenCalled();
});
it('Stop during receipt clear then late durable clear permits explicit retry without another POST',async()=>{
 const {api,props}=recoveryFixture();render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(1));let release!:()=>void;removal.pause=()=>new Promise<void>(done=>{release=done});fireEvent.click(screen.getByText('Start another batch'));await waitFor(()=>expect(release).toBeDefined());fireEvent.click(screen.getByText('Stop loading'));await act(async()=>release());removal.pause=null;fireEvent.click(screen.getByText('Retry loading'));await waitFor(()=>expect(screen.queryByText('Start another batch')).toBeNull());expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);expect(screen.getByText('Prepare batch review')).toBeTruthy();
});
it('retained old receipt-clear Stop cannot cancel the replacement acknowledgement',async()=>{
 const {api,props}=recoveryFixture();render(<PersistentSemanticCreate {...props} mask={mask}/>);fireEvent.click(await screen.findByText('Prepare batch review'));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledTimes(1));let release!:()=>void;removal.pause=()=>new Promise<void>(done=>{release=done});fireEvent.click(screen.getByText('Start another batch'));await waitFor(()=>expect(release).toBeDefined());const oldStop=renderedStops.at(-1)!;fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry loading'));await act(async()=>oldStop());expect(screen.getByText('Stop loading')).toBeTruthy();removal.pause=null;await act(async()=>release());await waitFor(()=>expect(screen.queryByText('Start another batch')).toBeNull());expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);if(label==='Stop loading')renderedStops.push(props.onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
