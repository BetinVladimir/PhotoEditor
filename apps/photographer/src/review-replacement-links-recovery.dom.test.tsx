// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {ComponentType} from 'react';
import type {OwnerAPI} from './owner-api';
vi.mock('react-native',async()=>import('react-native-web'));
import {PerspectiveReplacementLinks} from './perspective-replacement-links';
import {MultiReplacementLinks} from './multi-replacement-links';
const cases=[['perspectiveReplacementPage',PerspectiveReplacementLinks],['multiReplacementPage',MultiReplacementLinks]] as const;
const row=(id:string)=>({originalTask:id,project:'project',operation:'panorama',scope:{}});
afterEach(()=>{cleanup();vi.useRealTimers();});
it.each(cases)('%s retries its exact failed page rather than starting another replacement',async(method,Action)=>{
 const read=vi.fn().mockResolvedValueOnce({items:[row('first')],next:'page2'}).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({items:[row('second')],next:null}),Component=Action as ComponentType<any>,onOpen=vi.fn();
 render(<Component api={{[method]:read} as unknown as OwnerAPI} tenant="tenant" review="review" decisionHash={'a'.repeat(64)} onOpen={onOpen}/>);fireEvent.click(await screen.findByText('Next replacements'));fireEvent.click(await screen.findByText('Retry replacements'));await screen.findByText('second');expect(read.mock.calls.map(call=>call[2])).toEqual([null,'page2','page2']);expect(onOpen).not.toHaveBeenCalled();
});
it.each(cases)('%s refuses cancelled late page data and retries readonly',async(method,Action)=>{
 let finish!:(value:unknown)=>void;const read=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({items:[row('current')],next:null}),Component=Action as ComponentType<any>;
 render(<Component api={{[method]:read} as unknown as OwnerAPI} tenant="tenant" review="review" decisionHash={'a'.repeat(64)} onOpen={vi.fn()}/>);await waitFor(()=>expect(read).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Cancel loading replacements'));await act(async()=>finish({items:[row('late')],next:null}));expect(screen.queryByText('late')).toBeNull();fireEvent.click(screen.getByText('Retry replacements'));await screen.findByText('current');
});
it.each(cases)('%s exposes retry after a noncooperative read timeout',async(method,Action)=>{
 vi.useFakeTimers();const read=vi.fn(()=>new Promise<never>(()=>{})),Component=Action as ComponentType<any>;
 render(<Component api={{[method]:read} as unknown as OwnerAPI} tenant="tenant" review="review" decisionHash={'a'.repeat(64)} onOpen={vi.fn()}/>);await act(async()=>vi.advanceTimersByTimeAsync(8000));expect(screen.getByText('Retry replacements')).toBeTruthy();
});
