// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import {InterchangeImportFormatScreen} from './interchange-import-format-screen';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(cleanup);
it.each(['photoeditor_bundle','xmp','captureone_sidecar'])('selects %s explicitly once without automatic navigation',format=>{
 const onSelect=vi.fn();render(<InterchangeImportFormatScreen tenant={id(1)} project={id(2)} onSelect={onSelect}/>);
 expect(onSelect).not.toHaveBeenCalled();const button=screen.getByText(`interchange_format_${format}`);fireEvent.click(button);fireEvent.click(button);
 expect(onSelect).toHaveBeenCalledTimes(1);expect(onSelect).toHaveBeenCalledWith(format);
});
it.each([{tenant:'invalid',project:id(2)},{tenant:id(1),project:'invalid'}])('rejects invalid scope without choices',scope=>{
 const onSelect=vi.fn();render(<InterchangeImportFormatScreen {...scope} onSelect={onSelect}/>);
 expect(screen.getByRole('alert')).toBeTruthy();expect(screen.queryAllByRole('button')).toHaveLength(0);expect(onSelect).not.toHaveBeenCalled();
});
