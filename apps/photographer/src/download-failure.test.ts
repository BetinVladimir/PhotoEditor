import {describe,it,expect} from 'vitest';
import {downloadFailureItems} from './download-failure';
describe('download failure report',()=>{
 it('shows explicit per-file terminal reasons',()=>{expect(downloadFailureItems({code:'DOWNLOAD_EXPORT_FAILED',items:[{ordinal:4,code:'EXPORT_FAILED'}]})).toEqual([{ordinal:4,code:'EXPORT_FAILED'}])});
 it('shows archive item failures',()=>{expect(downloadFailureItems({code:'DOWNLOAD_ARCHIVE_FAILED',items:[{ordinal:2,code:'ARCHIVE_FAILED'}]})).toEqual([{ordinal:2,code:'ARCHIVE_FAILED'}])});
 it('refuses malformed and duplicate reports',()=>{for(const items of [[{ordinal:-1,code:'EXPORT_FAILED'}],[{ordinal:0,code:'RAW_PRIVATE_ERROR'}],[{ordinal:0,code:'EXPORT_FAILED'},{ordinal:0,code:'EXPORT_SKIPPED'}]])expect(downloadFailureItems({code:'DOWNLOAD_EXPORT_FAILED',items})).toEqual([])});
});

it('shows terminal unavailable archive output without exposing raw errors',()=>{expect(downloadFailureItems({code:'DOWNLOAD_ARCHIVE_FAILED',items:[{ordinal:0,code:'ARCHIVE_OUTPUT_UNAVAILABLE'}]})).toEqual([{ordinal:0,code:'ARCHIVE_OUTPUT_UNAVAILABLE'}])});
