import {expect,it} from 'vitest';
import {ownerUploadFilename} from './upload-filename';
it.each([' ', '\t', '\n', '\u0085', '\u00a0', '\u1680', '\u2003', '\u2028', '\u202f', '\u205f', '\u3000'])('matches server trimming for %j',space=>{
 expect(ownerUploadFilename(space+'frame.nef'+space)).toBe('frame.nef');
});
it('preserves interior whitespace and characters not trimmed by Go',()=>{
 expect(ownerUploadFilename('  frame name.nef  ')).toBe('frame name.nef');
 expect(ownerUploadFilename('\ufeffframe.nef\ufeff')).toBe('\ufeffframe.nef\ufeff');
 expect(ownerUploadFilename('')).toBe('');
});
