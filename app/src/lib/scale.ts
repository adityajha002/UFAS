import { useMemo } from 'react';
import { Dimensions, StyleSheet, useWindowDimensions } from 'react-native';

/** Sizes in the code are pixels on the 1080-px-wide mockup. */
export const ARTBOARD_WIDTH = 1080;

/**
 * Widest the layout gets (dp). Phones use their full width; tablets and browsers get a centred
 * phone-width column (see src/app/index.tsx) instead of a stretched layout.
 */
export const MAX_CONTENT_WIDTH = 480;

export const contentWidth = (windowWidth: number) => Math.min(windowWidth, MAX_CONTENT_WIDTH);

export type Scale = (px: number) => number;

/** Convert a mockup pixel measurement to the content width. */
export const scaleFor = (windowWidth: number): Scale => (px: number) =>
  (px * contentWidth(windowWidth)) / ARTBOARD_WIDTH;

/** Scale fixed at the width the app started with. Prefer makeStyles() so sizes follow the window. */
export const s = (px: number) => scaleFor(Dimensions.get('window').width)(px);

/**
 * Style sheet that is rebuilt when the window width changes (rotation, split screen, browser resize).
 *   const useStyles = makeStyles((s, width) => ({ box: { padding: s(40) } }));
 *   function C() { const styles = useStyles(); ... }
 * `width` is the content width in dp.
 */
export function makeStyles<T extends StyleSheet.NamedStyles<T>>(build: (s: Scale, width: number) => T) {
  return function useStyles(): T {
    const { width } = useWindowDimensions();
    return useMemo(() => StyleSheet.create(build(scaleFor(width), contentWidth(width))), [width]);
  };
}

/** The live scale and content width (dp), for sizes computed in render. */
export function useLayout(): { s: Scale; width: number } {
  const { width } = useWindowDimensions();
  return useMemo(() => ({ s: scaleFor(width), width: contentWidth(width) }), [width]);
}

/** Width of each day chip so three fit across a week card (see WeekCard). */
export function chipWidth(s: Scale, width: number): number {
  const card = width - 2 * s(SIDE);
  const inner = card - 2 * s(WEEK_PAD);
  return Math.floor((inner - 6 * s(CHIP_GAP)) / 3);
}

/** Week card inner padding and the margin around each chip (mockup px). */
export const WEEK_PAD = 30;
export const CHIP_GAP = 10;

/** Side margin shared by the greeting card, section titles and week cards. */
export const SIDE = 60;
