// @vitest-environment jsdom
import React from 'react';
import {it,expect,vi,afterEach} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import {PerspectiveReplacementLinks} from './perspective-replacement-links';
vi.mock('react-native',()=>({View:({children}:any)=><div>{children}</div>,Text:({children,accessibilityRole}:any)=><span role={accessibilityRole}>{children}</span>,Pressable:({children,onPress}:any)=><button onClick={onPress}>{children}</button>}));
afterEach(cleanup);
const props={tenant:'tenant',review:'review',decisionHash:'hash'};
it('opens only a currently scoped replacement and paginates/refreshes',async()=>{
 const onOpen=vi.fn(),page={review:'review',decisionHash:'hash',items:[{originalTask:'original',project:'project',scope:{}},{originalTask:'unavailable',project:'project',scope:null}],next:'cursor'};
 const api={perspectiveReplacementPage:vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce({...page,items:[],next:null}).mockResolvedValue(page)} as unknown as OwnerAPI;
 render(<PerspectiveReplacementLinks {...props} api={api} onOpen={onOpen}/>);
 fireEvent.click(await screen.findByText('Review perspective replacement'));
 expect(onOpen).toHaveBeenCalledWith('project','original');expect(screen.getByText('Replacement is not available yet.')).toBeTruthy();
 fireEvent.click(screen.getByText('Next replacements'));await screen.findByText('No perspective replacements on this page.');
 expect(api.perspectiveReplacementPage).toHaveBeenLastCalledWith('review','hash','cursor',expect.any(AbortSignal));
 fireEvent.click(screen.getByText('Refresh replacements'));await screen.findByText('Review perspective replacement');
 expect(api.perspectiveReplacementPage).toHaveBeenLastCalledWith('review','hash',null,expect.any(AbortSignal));
});
it('aborts old client list and discards its late result',async()=>{
 let finish!:(value:unknown)=>void;
 const first={perspectiveReplacementPage:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))} as unknown as OwnerAPI;
 const next={perspectiveReplacementPage:vi.fn().mockRejectedValue(new Error('revoked'))} as unknown as OwnerAPI;
 const view=render(<PerspectiveReplacementLinks {...props} api={first} onOpen={vi.fn()}/>);
 await waitFor(()=>expect(first.perspectiveReplacementPage).toHaveBeenCalledTimes(1));
 const signal=(first.perspectiveReplacementPage as any).mock.calls[0][3] as AbortSignal;
 view.rerender(<PerspectiveReplacementLinks {...props} api={next} onOpen={vi.fn()}/>);expect(signal.aborted).toBe(true);
 finish({review:'review',decisionHash:'hash',items:[{originalTask:'old',project:'old',scope:{}}],next:null});
 await screen.findByRole('alert');expect(screen.queryByText('Review perspective replacement')).toBeNull();
});
