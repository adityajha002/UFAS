// The only file in the app that imports the Kotlin module. Mock this in tests.
import native from '../../modules/attendance-security';
export type {
  AttendAction, AttendanceFields, AttendanceV2Fields, HistoryFields, KeyInfo, SecurityLevel,
} from '../../modules/attendance-security';
export default native;
