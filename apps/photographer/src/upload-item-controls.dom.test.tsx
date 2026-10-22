// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('./locale-context',async()=>{const __factory=async()=>{const en=await import('../../../locales/en/common.json');return {useLocale:()=>({t:(key:keyof typeof en.default)=>en.default[key]})}};const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('react-native',async()=>import('react-native-web'));
import {UploadItemControls} from './upload-item-controls';
afterEach(cleanup);
it('offers pause and cancel while uploading and blocks repeated busy actions',()=>{
 const onPause=vi.fn(),onAbort=vi.fn();
 const view=render(<UploadItemControls state="uploading" busy={false} onPause={onPause} onAbort={onAbort}/>);
 fireEvent.click(screen.getByRole('button',{name:'Pause'}));fireEvent.click(screen.getByRole('button',{name:'Cancel upload'}));expect(onPause).toHaveBeenCalledTimes(1);expect(onAbort).toHaveBeenCalledTimes(1);
 view.rerender(<UploadItemControls state="uploading" busy={true} onPause={onPause} onAbort={onAbort}/>);
 fireEvent.click(screen.getByRole('button',{name:'Pause'}));fireEvent.click(screen.getByRole('button',{name:'Cancel upload'}));expect(onPause).toHaveBeenCalledTimes(1);expect(onAbort).toHaveBeenCalledTimes(1);
});
it.each(['complete','aborted'] as const)('has no destructive controls for %s',state=>{
 render(<UploadItemControls state={state} busy={false} onPause={vi.fn()} onAbort={vi.fn()}/>);expect(screen.queryByRole('button')).toBeNull();
});
it('shows the same-file recovery hint for a paused upload',()=>{
 render(<UploadItemControls state="paused" busy={false} onPause={vi.fn()} onAbort={vi.fn()}/>);expect(screen.getByText('Select the same file to resume')).toBeTruthy();expect(screen.queryByRole('button',{name:'Pause'})).toBeNull();
});

it.each(['paused','error','completing'] as const)('offers explicit server reconciliation for %s when identity is known',state=>{
 const onRefresh=vi.fn();const view=render(<UploadItemControls state={state} busy={false} onPause={vi.fn()} onAbort={vi.fn()} onRefresh={onRefresh}/>);
 fireEvent.click(screen.getByRole('button',{name:'Check upload status'}));expect(onRefresh).toHaveBeenCalledTimes(1);
 view.rerender(<UploadItemControls state={state} busy={true} onPause={vi.fn()} onAbort={vi.fn()} onRefresh={onRefresh}/>);
 fireEvent.click(screen.getByRole('button',{name:'Check upload status'}));expect(onRefresh).toHaveBeenCalledTimes(1);
});

it('explains new upload recovery and hides invalid cancel for a terminal server error',()=>{
 render(<UploadItemControls state="error" restartRequired busy={false} onPause={vi.fn()} onAbort={vi.fn()}/>);expect(screen.getByText('Select the file again to start a new upload.')).toBeTruthy();expect(screen.queryByRole('button',{name:'Cancel upload'})).toBeNull();
});
