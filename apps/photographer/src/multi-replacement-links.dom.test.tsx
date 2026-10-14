// @vitest-environment jsdom
import React from 'react';
import {it,expect,vi,afterEach} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import {MultiReplacementLinks} from './multi-replacement-links';
vi.mock('react-native',()=>({View:({children}:any)=><div>{children}</div>,Text:({children,accessibilityRole}:any)=><span role={accessibilityRole}>{children}</span>,Pressable:({children,onPress}:any)=><button onClick={onPress}>{children}</button>}));
afterEach(cleanup);
const props={tenant:'tenant',review:'review',decisionHash:'hash'};
it('opens only a currently scoped replacement and paginates/refreshes',async()=>{
 const onOpen=vi.fn(),page={review:'review',decisionHash:'hash',items:[{originalTask:'original',project:'project',operation:'optical.panorama',scope:{}},{originalTask:'unavailable',project:'project',operation:'optical.focus_stack',scope:null}],next:'cursor'};
 const api={multiReplacementPage:vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce({...page,items:[],next:null}).mockResolvedValue(page)} as unknown as OwnerAPI;
 render(<MultiReplacementLinks {...props} api={api} onOpen={onOpen}/>);
 fireEvent.click(await screen.findByText('Review replacement'));
 expect(onOpen).toHaveBeenCalledWith('project','original','optical.panorama');expect(screen.getByText('Replacement is not available yet.')).toBeTruthy();
 fireEvent.click(screen.getByText('Next replacements'));await screen.findByText('No panorama or focus stack replacements on this page.');
 expect(api.multiReplacementPage).toHaveBeenLastCalledWith('review','hash','cursor',expect.any(AbortSignal));
 fireEvent.click(screen.getByText('Refresh replacements'));await screen.findByText('Review replacement');
 expect(api.multiReplacementPage).toHaveBeenLastCalledWith('review','hash',null,expect.any(AbortSignal));
});
it('aborts old client list and discards its late result',async()=>{
 let finish!:(value:unknown)=>void;
 const first={multiReplacementPage:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))} as unknown as OwnerAPI;
 const next={multiReplacementPage:vi.fn().mockRejectedValue(new Error('revoked'))} as unknown as OwnerAPI;
 const view=render(<MultiReplacementLinks {...props} api={first} onOpen={vi.fn()}/>);
 await waitFor(()=>expect(first.multiReplacementPage).toHaveBeenCalledTimes(1));
 const signal=(first.multiReplacementPage as any).mock.calls[0][3] as AbortSignal;
 view.rerender(<MultiReplacementLinks {...props} api={next} onOpen={vi.fn()}/>);expect(signal.aborted).toBe(true);
 finish({review:'review',decisionHash:'hash',items:[{originalTask:'old',project:'old',operation:'optical.focus_stack',scope:{}}],next:null});
 await screen.findByRole('alert');expect(screen.queryByText('Review replacement')).toBeNull();
});

it('passes focus-stack operation to navigation without deriving it from the task',async()=>{
 const onOpen=vi.fn(),api={multiReplacementPage:vi.fn().mockResolvedValue({review:'review',decisionHash:'hash',items:[{originalTask:'focus',project:'project',operation:'optical.focus_stack',scope:{}}],next:null})} as unknown as OwnerAPI;
 render(<MultiReplacementLinks {...props} api={api} onOpen={onOpen}/>);
 fireEvent.click(await screen.findByText('Review replacement'));
 expect(onOpen).toHaveBeenCalledWith('project','focus','optical.focus_stack');
});
