// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import {InterchangeExportFormatScreen} from './interchange-export-format-screen';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(cleanup);
it.each(['photoeditor_bundle','xmp','captureone_sidecar'])('passes explicit %s with the same frozen snapshot',format=>{
 const onSelect=vi.fn();render(<InterchangeExportFormatScreen tenant={id(1)} project={id(2)} snapshot={id(3)} onSelect={onSelect}/>);
 expect(onSelect).not.toHaveBeenCalled();fireEvent.click(screen.getByText(`interchange_format_${format}`));
 expect(onSelect).toHaveBeenCalledWith({project_id:id(2),direction:'export',format,snapshot_id:id(3)});
});
it('does not offer choices for invalid scope',()=>{
 const onSelect=vi.fn();render(<InterchangeExportFormatScreen tenant={id(1)} project={id(2)} snapshot="invalid" onSelect={onSelect}/>);
 expect(screen.getByRole('alert')).toBeTruthy();expect(screen.queryAllByRole('button')).toHaveLength(0);expect(onSelect).not.toHaveBeenCalled();
});
it('navigates once per frozen export scope and accepts a new keyed scope',()=>{
 const onSelect=vi.fn();
 const first=<InterchangeExportFormatScreen key={id(3)} tenant={id(1)} project={id(2)} snapshot={id(3)} onSelect={onSelect}/>;
 const view=render(first);
 fireEvent.click(screen.getByText('interchange_format_xmp'));
 fireEvent.click(screen.getByText('interchange_format_captureone_sidecar'));
 expect(onSelect).toHaveBeenCalledTimes(1);
 view.rerender(<InterchangeExportFormatScreen key={id(4)} tenant={id(1)} project={id(2)} snapshot={id(4)} onSelect={onSelect}/>);
 fireEvent.click(screen.getByText('interchange_format_captureone_sidecar'));
 expect(onSelect).toHaveBeenCalledTimes(2);
 expect(onSelect).toHaveBeenLastCalledWith({project_id:id(2),direction:'export',format:'captureone_sidecar',snapshot_id:id(4)});
});
