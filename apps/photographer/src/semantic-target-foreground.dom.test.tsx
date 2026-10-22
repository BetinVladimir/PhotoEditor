// @vitest-environment jsdom
import React from 'react';
import {afterEach,it,expect,vi} from 'vitest';
import {act,cleanup,render,screen,fireEvent,waitFor} from '@testing-library/react';
const app=vi.hoisted(()=>({state:'active',listeners:new Set<(state:string)=>void>()}));
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canEdit:true,actor:'actor'})}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return app.state},addEventListener:(_event:string,listener:(state:string)=>void)=>{app.listeners.add(listener);return{remove:()=>app.listeners.delete(listener)}}}}));
import {SemanticTargetScreen} from './semantic-target-screen';
const id='11111111-1111-4111-8111-111111111111';
function change(state:string){act(()=>{app.state=state;app.listeners.forEach(listener=>listener(state))})}
afterEach(()=>{cleanup();app.state='active';app.listeners.clear()});
it('does not read targets in initial background and loads on foreground',async()=>{
 app.state='background';const semanticTargets=vi.fn().mockResolvedValue({items:[],next_cursor:null});
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await act(async()=>{});fireEvent.click(screen.getByRole('button',{name:'Refresh'}));expect(semanticTargets).not.toHaveBeenCalled();
 change('active');await waitFor(()=>expect(semanticTargets).toHaveBeenCalledOnce());
});
it('detaches hung background read and ignores late old page after fresh foreground load',async()=>{
 let finish!:(value:unknown)=>void;
 const semanticTargets=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({items:[],next_cursor:null});
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(semanticTargets).toHaveBeenCalledOnce());const signal=semanticTargets.mock.calls[0][2].signal as AbortSignal;
 change('background');expect(signal.aborted).toBe(true);change('active');await waitFor(()=>expect(semanticTargets).toHaveBeenCalledTimes(2));
 await act(async()=>finish({items:[],next_cursor:'must-not-install'}));expect(screen.queryByRole('button',{name:'Load more'})).toBeNull();expect(screen.queryByRole('alert')).toBeNull();
 expect(semanticTargets.mock.calls[1][2].cursor).toBeUndefined();
});
