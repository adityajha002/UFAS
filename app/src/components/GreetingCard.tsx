import React from 'react';
import { Text, View } from 'react-native';
import { colors } from '../theme/colors';
import { makeStyles, SIDE } from '../lib/scale';
import { greeting } from '../lib/greeting';
import { MarkAttendanceButton, MarkState } from './MarkAttendanceButton';

type Props = {
  state: MarkState;
  /** Button text override ("MARK IN" / "MARK OUT"). */
  label?: string;
  message?: string;
  blockName?: string;
  /** IN/OUT flow: instruction text mentions entry and exit. */
  inOut?: boolean;
  /** Today's state, e.g. "Today: IN since 09:12 · worked 3h 20m". */
  today?: string | null;
  onMark: () => void;
};

export function GreetingCard({ state, label, message, blockName = 'E BLOCK', inOut = false, today, onMark }: Props) {
  const styles = useStyles();
  return (
    <View style={styles.card}>
      <View style={styles.top}>
        <Text style={styles.greeting}>{greeting()}</Text>
        <Text style={styles.body}>
          {inOut
            ? 'PLEASE CLICK THE BUTTON BELOW TO RECORD YOUR ENTRY OR EXIT'
            : 'PLEASE CLICK THE BUTTON BELOW TO UPLOAD THE ATTENDANCE'}
        </Text>
        <Text style={styles.body}>ATTENDANCE IS VERIFIED BY GEO-FENCING, PLEASE MAKE SURE YOU ARE IN {blockName}</Text>
        <Text style={styles.body}>THIS PROCESS MAY TAKE A FEW SECONDS.</Text>
        {today ? <Text style={styles.today}>{today}</Text> : null}
        {message ? <Text style={styles.status} accessibilityLiveRegion="polite">{message}</Text> : null}
      </View>
      <View style={styles.bottom}>
        <MarkAttendanceButton state={state} label={label} onPress={onMark} />
      </View>
    </View>
  );
}

const useStyles = makeStyles((s) => ({
  card: { marginHorizontal: s(SIDE), borderRadius: s(44), backgroundColor: colors.orange, overflow: 'hidden', borderWidth: s(6), borderColor: colors.orange },
  top: { backgroundColor: colors.navyCard, paddingHorizontal: s(50), paddingTop: s(46), paddingBottom: s(40) },
  greeting: { color: colors.white, fontSize: s(66), fontWeight: '900', marginBottom: s(18) },
  body: { color: colors.white, fontSize: s(34), lineHeight: s(50), opacity: 0.92 },
  today: {
    color: colors.white, fontSize: s(38), fontWeight: '800', marginTop: s(26),
    paddingTop: s(22), borderTopWidth: 1, borderTopColor: 'rgba(255,255,255,0.25)',
  },
  status: { color: colors.yellow, fontSize: s(38), lineHeight: s(52), marginTop: s(14), fontWeight: '700' },
  bottom: { paddingHorizontal: s(40), paddingVertical: s(32) },
}));
