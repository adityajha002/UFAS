import React, { useMemo } from 'react';
import { ActivityIndicator, Pressable, Text, View } from 'react-native';
import { colors } from '../theme/colors';
import { makeStyles, SIDE } from '../lib/scale';
import { groupByWeek, monthSummary, monthSummaryText } from '../lib/groupByWeek';
import { buttonBase, updatedLabel } from '../lib/historyView';
import { Header } from '../components/Header';
import { GreetingCard } from '../components/GreetingCard';
import { AttendanceHistory } from '../components/AttendanceHistory';
import { StatusLegend } from '../components/StatusLegend';
import { dayDetailLookup, hasDayStatus } from '../lib/dayStatus';
import { useMarkAttendance } from '../hooks/useMarkAttendance';
import { useMarkInOut } from '../hooks/useMarkInOut';
import { useHistory } from '../hooks/useHistory';
import { useRequestOut } from '../hooks/useRequestOut';
import { inOutBase, inOutButton, inOutMessage, markMode, todayLine, windowSummary } from '../lib/inOut';

type Props = {
  facultyId: string;
  name?: string | null;
  /** Revoked or key missing: clear local enrolment and go back to the enrolment screen. */
  onNeedsEnrol: () => void;
};

export default function HomeScreen({ facultyId, name, onNeedsEnrol }: Props) {
  const styles = useStyles();
  const h = useHistory(facultyId);
  const now = Date.now();
  const mode = markMode(h.data, h.fresh);

  // v1: an older server (no `is_in` in /history) keeps the original single daily mark, unchanged.
  const base = buttonBase(h.data, h.savedAt, now, h.fresh);
  const v1 = useMarkAttendance({ facultyId, base, onMarked: h.refresh });

  // v2: MARK IN / MARK OUT from the server's `is_in`; unknown until /history answers this session.
  const io = inOutBase(h.data, h.fresh);
  const requestAction = (io.action === 'request_in' || io.action === 'request_out') && !io.pending;
  const requestType = io.action === 'request_in' ? 'in' : io.action === 'request_out' ? 'out' : null;
  const v2 = useMarkInOut({ facultyId, action: io.action === 'request_in' || io.action === 'request_out' ? null : io.action, onDone: h.afterMark });
  const request = useRequestOut({ facultyId, action: requestType ?? 'out', onDone: () => h.afterMark() });
  const v2Button = inOutButton(io, v2.busy || request.busy, h.status);

  const card = mode === 'v1'
    ? { state: v1.state, message: v1.message, onMark: v1.mark }
    : {
      state: v2Button.state,
      label: v2Button.label,
      message: requestAction ? request.notice ?? inOutMessage(io, h.status, request.stage, request.notice) : inOutMessage(io, h.status, v2.stage, v2.notice),
      onMark: requestAction ? request.mark : v2.mark,
      inOut: true,
      today: todayLine(h.data, h.fresh),
    };

  const schedule = windowSummary(h.data, h.fresh);

  const weeks = useMemo(() => {
    const t = Date.now();
    return groupByWeek(dayDetailLookup(h.data, t), t);
  }, [h.data]);
  const summary = useMemo(() => monthSummaryText(monthSummary(weeks, Date.now())), [weeks]);
  const shownName = h.data?.name ?? name;

  if (h.status === 'revoked') {
    return (
      <View style={[styles.page, styles.centre]}>
        <Text style={styles.big}>This device is no longer enrolled</Text>
        <Text style={styles.msg}>Please visit the office. If you have been given a new code you can enrol again.</Text>
        <Pressable style={styles.btn} onPress={onNeedsEnrol}><Text style={styles.btnText}>ENROL WITH A NEW CODE</Text></Pressable>
      </View>
    );
  }
  if (h.status === 'key_missing') {
    return (
      <View style={[styles.page, styles.centre]}>
        <Text style={styles.big}>This device needs to be enrolled again</Text>
        <Text style={styles.msg}>The secure key on this phone is missing. Ask the office for a new code.</Text>
        <Pressable style={styles.btn} onPress={onNeedsEnrol}><Text style={styles.btnText}>ENROL AGAIN</Text></Pressable>
      </View>
    );
  }

  const stale = h.status === 'offline' || h.status === 'error';
  const banner = !stale ? null
    : h.status === 'offline' ? 'You are offline. Showing saved history.' : h.error;

  return (
    <View style={styles.page}>
      <Header name={shownName} />
      <GreetingCard {...card} />
      {schedule.length > 0 && (
        <View style={styles.scheduleBox}>
          {schedule.map((line) => (
            <Text key={line} style={styles.scheduleText}>{line}</Text>
          ))}
        </View>
      )}
      {h.data && banner && (
        <Text style={styles.banner} accessibilityRole="alert">
          {banner}{h.savedAt ? ` Last updated ${updatedLabel(h.savedAt, now)}.` : ''}
        </Text>
      )}
      <Text style={styles.section}>PREVIOUSLY MARKED ATTENDANCE</Text>
      {h.data ? <Text style={styles.summary}>{summary}</Text> : null}
      {hasDayStatus(h.data) ? <StatusLegend /> : null}

      {!h.data && h.status === 'loading' ? (
        <View style={styles.centre}><ActivityIndicator color={colors.navyHeader} /></View>
      ) : !h.data ? (
        <View style={styles.centre}>
          <Text style={styles.msg}>{h.status === 'offline' ? 'You are offline and there is no saved history yet.' : h.error}</Text>
          <Pressable style={styles.btn} onPress={h.refresh}><Text style={styles.btnText}>TRY AGAIN</Text></Pressable>
        </View>
      ) : (
        <AttendanceHistory weeks={weeks} refreshing={h.pulling} onRefresh={h.pull} />
      )}
    </View>
  );
}

const useStyles = makeStyles((s) => ({
  page: { flex: 1, backgroundColor: colors.cream },
  centre: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: s(60) },
  big: { color: colors.navyHeader, fontSize: s(58), fontWeight: '900', textAlign: 'center', marginBottom: s(24) },
  msg: { color: colors.textDark, fontSize: s(40), lineHeight: s(56), textAlign: 'center' },
  btn: { minHeight: 48, backgroundColor: colors.yellow, borderRadius: s(24), paddingHorizontal: s(48), paddingVertical: s(28), marginTop: s(40), alignItems: 'center', justifyContent: 'center' },
  btnText: { color: colors.navyHeader, fontSize: s(40), fontWeight: '800', letterSpacing: 1 },
  banner: { color: '#7A3E00', fontSize: s(34), lineHeight: s(48), marginHorizontal: s(SIDE), marginTop: s(28) },
  scheduleBox: { marginHorizontal: s(SIDE), marginTop: s(10), paddingHorizontal: s(28), paddingVertical: s(18), backgroundColor: '#F4E7D2', borderRadius: s(20) },
  scheduleText: { color: colors.navyHeader, fontSize: s(34), lineHeight: s(48), fontWeight: '700' },
  section: { color: colors.navyHeader, fontSize: s(48), fontWeight: '900', marginHorizontal: s(SIDE), marginTop: s(44), marginBottom: s(10) },
  summary: { color: colors.navyHeader, fontSize: s(32), fontWeight: '700', opacity: 0.8, marginHorizontal: s(SIDE), marginBottom: s(22) },
}));
