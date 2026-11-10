// Approximate values read from the mockup; replace with the designer's tokens.
export const colors = {
  navyHeader: '#0B0044', navyCard: '#1B1455', yellow: '#FFE05A', orange: '#FF9A2E',
  cream: '#F8F1DF', historyBg: '#C5C1B2', green: '#00C060', chipGrey: '#DCDCDC',
  white: '#FFFFFF', textDark: '#111111', buttonDisabledText: '#7A7A7A',
  // Day status (APP-3). Placeholders until 00_OVERVIEW.md §4 gives the real values; both keep
  // >= 4.5:1 contrast with textDark and stay distinct from `orange` and `yellow`.
  amber: '#FFB300', red: '#EF5350',
} as const;
