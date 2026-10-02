import {describe,it,expect} from 'vitest';
import {readEventStream} from './sse';
const bytes=new TextEncoder();
const stream=(chunks:string[])=>new ReadableStream<Uint8Array>({start(controller){for(const chunk of chunks)controller.enqueue(bytes.encode(chunk));controller.close()}});
describe('SSE durable envelopes',()=>{
 it('reads fragmented CRLF frames and ignores heartbeats',async()=>{
  const events:unknown[]=[];
  await readEventStream(stream([': heartbeat\r\n\r\nid: 12\r','\nevent: message\r\ndata: {"cursor":"12","type":"capture.alert",','"payload":{}}\r\n\r\n']),event=>events.push(event),new AbortController().signal);
  expect(events).toEqual([{cursor:'12',type:'capture.alert',payload:{}}]);
 });
 it('rejects a cursor that differs from the durable envelope',async()=>{
  await expect(readEventStream(stream(['id: 1\ndata: {"cursor":"2","type":"capture.alert"}\n\n']),()=>{},new AbortController().signal)).rejects.toThrow('Invalid SSE event');
 });
 it('does not dispatch an unfinished event at EOF',async()=>{
  const events:unknown[]=[];
  await readEventStream(stream(['id: 1\ndata: {"cursor":"1","type":"capture.alert"}\n']),event=>events.push(event),new AbortController().signal);
  expect(events).toEqual([]);
 });
 it('rejects oversized unfinished frames',async()=>{
  await expect(readEventStream(stream(['x'.repeat(1048577)]),()=>{},new AbortController().signal)).rejects.toThrow('SSE frame too large');
 });
 it('honors cancellation before any event',async()=>{
  const controller=new AbortController();controller.abort();
  await expect(readEventStream(stream([]),()=>{},controller.signal)).rejects.toThrow();
 });
});
