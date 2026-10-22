// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {ExportPresetForm} from './export-preset-form';
import type {ExportPresetConfig} from './catalog-types';
const value:ExportPresetConfig={format:'jpeg',icc_id:'srgb_v1',quality:92,subsampling:'444',alpha:'matte',matte_rgb:[1,1,1],metadata_policy:'strip',naming_template:'{original_name}_edited'};
afterEach(cleanup);
it('TIFF choice preserves frozen color and naming while removing JPEG-only quality',()=>{
 const change=vi.fn();render(<ExportPresetForm value={value} onChange={change} disabled={false}/>);
 fireEvent.click(screen.getByLabelText('Format: tiff'));
 expect(change.mock.calls[0][0]).toEqual({...value,format:'tiff',quality:undefined});
});
it('pending request cannot change any encoding field',()=>{
 const change=vi.fn();render(<ExportPresetForm value={value} onChange={change} disabled/>);
 fireEvent.click(screen.getByLabelText('Format: png'));
 fireEvent.change(screen.getByLabelText('File name pattern'),{target:{value:'new'}});
 expect(change).not.toHaveBeenCalled();
});
