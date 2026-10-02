// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {render,screen,act,cleanup,fireEvent} from '@testing-library/react';
import en from '../../../locales/en/common.json';
import type {RegionDisplay} from './region-preview-display';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
import {RegionLoupe} from './region-loupe';
afterEach(cleanup);
const display=():RegionDisplay=>({uri:'blob:region',width:100,height:50,scale:{x:.5,y:.5,sourcePixels:false},dispose:vi.fn()});
it('shows actual rendered scale and disposes the owned result on unmount',async()=>{
 const value=display(),load=vi.fn().mockResolvedValue(value);const view=render(<RegionLoupe scope="revision1" load={load}/>);
 await screen.findByText('Rendered scale 50% × 50%');expect(screen.getByLabelText(en.editor_loupe_image)).toBeDefined();view.unmount();expect(value.dispose).toHaveBeenCalledOnce();expect(load.mock.calls[0][0].aborted).toBe(true);
});
it('hides an old image immediately and disposes a late response after scope change',async()=>{
 let resolve!:(value:RegionDisplay)=>void;const old=display();const load=vi.fn().mockResolvedValueOnce(old).mockImplementationOnce(()=>new Promise(a=>{resolve=a;})).mockResolvedValueOnce(display());
 const view=render(<RegionLoupe scope="a" load={load}/>);await screen.findByLabelText(en.editor_loupe_image);
 view.rerender(<RegionLoupe scope="b" load={load}/>);expect(screen.queryByLabelText(en.editor_loupe_image)).toBeNull();expect(old.dispose).toHaveBeenCalledOnce();await act(async()=>{});
 view.rerender(<RegionLoupe scope="c" load={load}/>);const late=display();await act(async()=>{resolve(late);});expect(late.dispose).toHaveBeenCalledOnce();await screen.findByLabelText(en.editor_loupe_image);
});
it('shows localized failure without retaining a previous result',async()=>{render(<RegionLoupe scope="a" load={async()=>{throw new Error('network');}}/>);expect(await screen.findByRole('alert')).toBeDefined();expect(screen.getByText(en.editor_loupe_failed)).toBeDefined();expect(screen.queryByLabelText(en.editor_loupe_image)).toBeNull();});

it('does not start deferred IO after immediate unmount',async()=>{const load=vi.fn();const view=render(<RegionLoupe scope="a" load={load}/>);view.unmount();await act(async()=>{});expect(load).not.toHaveBeenCalled();});

it('retries the same loader explicitly after failure and disposes its verified result',async()=>{
 const result=display(),load=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(result);
 const view=render(<RegionLoupe scope="same-attempt" load={load}/>);await screen.findByRole('alert');
 fireEvent.click(screen.getByRole('button',{name:en.editor_loupe_retry}));await screen.findByLabelText(en.editor_loupe_image);
 expect(load).toHaveBeenCalledTimes(2);expect((load.mock.calls[0][0] as AbortSignal).aborted).toBe(true);
 expect(screen.queryByRole('alert')).toBeNull();view.unmount();expect(result.dispose).toHaveBeenCalledOnce();
});
