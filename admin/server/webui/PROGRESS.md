# Admin UI progress

## ADM-1: day view status + hours

The day panel now handles three attendance states without breaking legacy data:

- `COMPLETE`: standard present row, no extra status chip, hours shown when available.
- `IN_PROGRESS`: amber `IN` chip with the session range and elapsed hours.
- `INCOMPLETE`: red `Incomplete` chip, while still keeping the legacy row layout intact for older payloads.

Legacy rows remain compatible when `sessions` is missing: they keep the original time display and render hours as `—`.

The helper `fmtHours(seconds | null)` formats as `6h 05m`, `0m`, and `—` for null/unknown values.
