import React from 'react';
import { ActivityIndicator, Pressable, Text } from 'react-native';
import { colors } from '../theme/colors';
import { makeStyles } from '../lib/scale';

import type { MarkState } from '../lib/markState';
export type { MarkState };
const LABEL: Record<MarkState, string> = {
  idle: 'MARK ATTENDANCE', closed: 'MARK ATTENDANCE', marked: 'MARKED',
  working: '', success: 'MARKED', error: 'TRY AGAIN', unknown: 'MARK IN / OUT',
};

/**
 * Full-width action button. `label` replaces the default text (IN/OUT: "MARK IN" / "MARK OUT");
 * `state` still sets the look (yellow when it can be pressed, grey otherwise).
 */
export function MarkAttendanceButton({ state, label, onPress }: { state: MarkState; label?: string; onPress: () => void }) {
  const styles = useStyles();
  const enabled = state === 'idle' || state === 'error' || state === 'closed';
  return (
    <Pressable
      onPress={onPress}
      disabled={!enabled}
      accessibilityRole="button"
      accessibilityState={{ disabled: !enabled, busy: state === 'working' }}
      style={({ pressed }) => [styles.btn, enabled && styles.enabled, enabled && pressed && styles.pressed]}
    >
      {state === 'working'
        ? <ActivityIndicator color={colors.navyHeader} />
        : <Text style={[styles.text, enabled && styles.textEnabled]}>{label ?? LABEL[state]}</Text>}
    </Pressable>
  );
}

const useStyles = makeStyles((s) => ({
  btn: {
    minHeight: 48,
    height: s(132),
    alignSelf: 'stretch',
    paddingHorizontal: s(28),
    borderRadius: s(26),
    backgroundColor: colors.chipGrey,
    alignItems: 'center',
    justifyContent: 'center',
  },
  enabled: { backgroundColor: colors.yellow },
  pressed: { opacity: 0.85 },
  text: { fontSize: s(48), fontWeight: '900', color: colors.buttonDisabledText, letterSpacing: 1.5 },
  textEnabled: { color: colors.navyHeader },
}));
