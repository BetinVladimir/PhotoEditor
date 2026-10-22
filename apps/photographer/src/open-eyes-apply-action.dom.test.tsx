// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
import {APIError} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {OpenEyesApplyAction} from './open-eyes-apply-action';
const review={id:'review',kind:'portrait.open_eyes',state:'decided',decision:'accept',decision_hash:'hash'} as Review;
afterEach(cleanup);
it('opens only after the saved result and actor confirmation',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn(),applyOpenEyesSelected=vi.fn().mockResolvedValue(result);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyOpenEyesSelected} as unknown as OwnerAPI;
 render(<OpenEyesApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyOpenEyesSelected).toHaveBeenCalledTimes(1);
});
it('allows retry of the same decision after a lost response',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn();
 const applyOpenEyesSelected=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyOpenEyesSelected} as unknown as OwnerAPI;
 render(<OpenEyesApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyOpenEyesSelected.mock.calls.map(args=>args[0])).toEqual([review,review]);
});
it('does not navigate after the actor changes',async()=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),api={me:vi.fn().mockResolvedValueOnce({id:'first'}).mockResolvedValueOnce({id:'other'}),applyOpenEyesSelected:vi.fn().mockResolvedValue({})} as unknown as OwnerAPI;
 render(<OpenEyesApplyAction api={api} review={review} onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');expect(onAccessDenied).toHaveBeenCalledTimes(1);expect(onOpen).not.toHaveBeenCalled();
});
it.each([401,403,404,410])('withdraws parent authority and blocks retry on %s',async status=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),applyOpenEyesSelected=vi.fn().mockRejectedValue(new APIError(status,'DENIED'));
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyOpenEyesSelected} as unknown as OwnerAPI;
 render(<OpenEyesApplyAction api={api} review={review} onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onAccessDenied).toHaveBeenCalledTimes(1));
 fireEvent.click(screen.getByText('Save result and open editor'));
 expect(applyOpenEyesSelected).toHaveBeenCalledTimes(1);expect(onOpen).not.toHaveBeenCalled();
});
it('ignores a late save response after unmount',async()=>{
 let complete!:(value:unknown)=>void;const pending=new Promise(resolve=>{complete=resolve}),onOpen=vi.fn();
 const applyOpenEyesSelected=vi.fn().mockReturnValue(pending);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyOpenEyesSelected} as unknown as OwnerAPI;
 const view=render(<OpenEyesApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(applyOpenEyesSelected).toHaveBeenCalledTimes(1));view.unmount();complete({asset_version_id:'old'});
 await pending;expect(onOpen).not.toHaveBeenCalled();expect(applyOpenEyesSelected.mock.calls[0][1].aborted).toBe(true);
});
it('does not present a late response under another decision',async()=>{
 let complete!:(value:unknown)=>void;const pending=new Promise(resolve=>{complete=resolve}),onOpen=vi.fn();
 const applyOpenEyesSelected=vi.fn().mockReturnValue(pending),api={me:vi.fn().mockResolvedValue({id:'actor'}),applyOpenEyesSelected} as unknown as OwnerAPI;
 const view=render(<OpenEyesApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(applyOpenEyesSelected).toHaveBeenCalledTimes(1));
 view.rerender(<OpenEyesApplyAction api={api} review={{...review,id:'next',decision_hash:'next'}} onOpen={onOpen}/>);
 complete({asset_version_id:'old'});await pending;expect(onOpen).not.toHaveBeenCalled();
});
