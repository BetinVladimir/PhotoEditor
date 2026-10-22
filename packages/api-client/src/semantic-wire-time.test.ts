import {it,expect} from 'vitest';
import {semanticWireTime} from './semantic-wire-time';
it.each(['2026-10-17T12:13:14Z','2026-10-17T12:13:14.123456789+03:00','2026-10-17T12:13:14.1-04:30'])('accepts actual Go timestamp %s',value=>{expect(semanticWireTime(value)).toBe(true);});
it.each(['2026-10-17T12:13:14','2026-10-17T12:13Z','2026-10-17','1','2026-10-17T12:13:14.1234567890Z',null,[]])('refuses non-wire timestamp %s',value=>{expect(semanticWireTime(value)).toBe(false);});
