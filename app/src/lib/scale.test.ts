jest.mock('react-native', () => ({
  Dimensions: { get: () => ({ width: 390, height: 844 }) },
  StyleSheet: { create: <T,>(x: T) => x },
  useWindowDimensions: () => ({ width: 390, height: 844 }),
}));

import { chipWidth, contentWidth, MAX_CONTENT_WIDTH, s, scaleFor, SIDE } from './scale';

describe('scale', () => {
  it('phones use their full width; wider screens are capped to a phone-width column', () => {
    expect(contentWidth(390)).toBe(390);
    expect(contentWidth(1920)).toBe(MAX_CONTENT_WIDTH);
  });
  it('mockup pixels scale to the content width', () => {
    expect(scaleFor(1080)(100)).toBeCloseTo(100 * MAX_CONTENT_WIDTH / 1080);
    expect(scaleFor(540)(100)).toBeCloseTo(100 * MAX_CONTENT_WIDTH / 1080); // 540 > cap
    expect(scaleFor(360)(108)).toBeCloseTo(36);
    expect(s(108)).toBeCloseTo(39); // 390-wide window
  });
  it('a browser at 1920 px gets the same sizes as a 480-dp tablet', () => {
    expect(scaleFor(1920)(SIDE)).toBe(scaleFor(480)(SIDE));
  });
  it('three chips plus their gaps fit inside a week card', () => {
    for (const w of [320, 360, 390, 430, 480]) {
      const sc = scaleFor(w);
      const cw = chipWidth(sc, contentWidth(w));
      const used = 3 * cw + 6 * sc(10) + 2 * sc(30) + 2 * sc(SIDE);
      expect(cw).toBeGreaterThan(60);
      expect(used).toBeLessThanOrEqual(contentWidth(w));
    }
  });
});
