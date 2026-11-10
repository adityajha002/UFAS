import { attendV1Payload, attendV2Payload, attendRequestOutPayload, AttendAction, historyPayload } from './payload';

// Same vectors as PayloadBuilderTest.kt and the server's crypto tests.
describe('signed payloads', () => {
  it('v1 attend golden vector', () => {
    expect(attendV1Payload({ challengeId: 'c1', nonce: 'n1', facultyId: 'f1', latMicro: 28600000, lonMicro: 77200000, accuracyCm: 1500 }))
      .toBe('attend.v1|c1|n1|f1|28600000|77200000|1500');
  });

  it('v2 attend golden vector (00_OVERVIEW.md §5.1)', () => {
    expect(attendV2Payload({
      action: 'out', challengeId: 'c0ffee', nonce: 'bm9uY2U', facultyId: 'EMP001',
      latMicro: 28606827, lonMicro: 77209000, accuracyCm: 1500,
    })).toBe('attend.v2|out|c0ffee|bm9uY2U|EMP001|28606827|77209000|1500');
  });

  it('v2 attend with action in and negative coordinates', () => {
    expect(attendV2Payload({ action: 'in', challengeId: 'c0ffee', nonce: 'bm9uY2U', facultyId: 'EMP001', latMicro: -1, lonMicro: -2, accuracyCm: 0 }))
      .toBe('attend.v2|in|c0ffee|bm9uY2U|EMP001|-1|-2|0');
  });

  it.each(['', 'IN', 'Out', 'in|x', 'attend'])('v2 rejects action %j', (bad) => {
    expect(() => attendV2Payload({
      action: bad as any, challengeId: 'c', nonce: 'n', facultyId: 'f', latMicro: 1, lonMicro: 2, accuracyCm: 3,
    })).toThrow('bad_action');
  });

  it('request_out golden vector', () => {
    expect(attendRequestOutPayload({ challengeId: 'c0ffee', nonce: 'bm9uY2U', facultyId: 'EMP001', latMicro: 28606827, lonMicro: 77209000, accuracyCm: 1500 }))
      .toBe('attend.v2|request_out|c0ffee|bm9uY2U|EMP001|28606827|77209000|1500');
  });

  it('history golden vector', () => {
    expect(historyPayload({ challengeId: 'c1', nonce: 'n1', facultyId: 'f1' })).toBe('history.v1|c1|n1|f1');
  });
});
