import { describe, expect, it, vi, beforeEach } from "vitest";
import { addDays, diffDays, fmtDay, fmtHours, isYmd, firstOfMonth } from "../lib/dates";
import { buildPatch } from "../lib/config";
import { api, ApiError, NetworkError, filenameFromDisposition, setToken } from "../api";
import type { ConfigField } from "../types";

describe("dates", () => {
  it("steps across month, year and leap day", () => {
    expect(addDays("2026-10-31", 1)).toBe("2026-11-01");
    expect(addDays("2026-01-01", -1)).toBe("2025-12-31");
    expect(addDays("2024-02-28", 1)).toBe("2024-02-29");
    expect(addDays("2025-02-28", 1)).toBe("2025-03-01");
  });
  it("computes differences and validates", () => {
    expect(diffDays("2026-01-01", "2026-12-31")).toBe(364);
    expect(isYmd("2026-02-30")).toBe(false);
    expect(isYmd("2026-10-31")).toBe(true);
    expect(isYmd("31-10-2026")).toBe(false);
  });
  it("formats without the system clock or timezone", () => {
    expect(fmtDay("2026-10-30")).toBe("Fri 30 Oct 2026");
    expect(firstOfMonth("2026-10-31")).toBe("2026-10-01");
  });
  it("formats hours with nulls and zero", () => {
    expect(fmtHours(6 * 3600 + 5 * 60)).toBe("6h 05m");
    expect(fmtHours(null)).toBe("—");
    expect(fmtHours(0)).toBe("0m");
  });
});

const field = (o: Partial<ConfigField>): ConfigField => ({ key: "geofence.radius_m", label: "r", type: "float", value: 150, editable: true, ...o });

describe("config patch", () => {
  it("sends only changed editable keys, YAML-shaped", () => {
    const fields = [field({}), field({ key: "window.end", type: "time", value: "18:00" }), field({ key: "geofence.lat", value: 28.6 })];
    const r = buildPatch(fields, { "geofence.radius_m": "200", "window.end": "19:00", "geofence.lat": "28.6" });
    expect(r.patch).toEqual({ geofence: { radius_m: 200 }, window: { end: "19:00" } });
    expect(r.errors).toEqual({});
  });
  it("accepts 24:00, rejects bad times and numbers", () => {
    const fields = [field({ key: "window.end", type: "time", value: "18:00" }), field({})];
    expect(buildPatch(fields, { "window.end": "24:00" }).patch).toEqual({ window: { end: "24:00" } });
    const bad = buildPatch(fields, { "window.end": "25:00", "geofence.radius_m": "abc" });
    expect(Object.keys(bad.errors).sort()).toEqual(["geofence.radius_m", "window.end"]);
    expect(bad.empty).toBe(true);
  });
  it("ignores read-only fields", () => {
    const r = buildPatch([field({ key: "server.listen", editable: false, type: "string", value: "x" })], { "server.listen": "y" });
    expect(r.empty).toBe(true);
  });
});

describe("api client", () => {
  beforeEach(() => { setToken("secret"); vi.restoreAllMocks(); });
  const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });

  it("sends the bearer header and url-encodes employee ids", async () => {
    const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(json(200, { employee: {}, days: [] }));
    await api.userAttendance("E 1/2");
    const [url, init] = f.mock.calls[0];
    expect(String(url)).toBe("/admin/api/faculty/E%201%2F2/attendance");
    expect((init!.headers as Record<string, string>).Authorization).toBe("Bearer secret");
  });
  it("maps error bodies and Retry-After", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(json(429, { error: "too_many_attempts", message: "slow down" }, { "Retry-After": "42" }));
    await expect(api.login("x")).rejects.toMatchObject({ status: 429, code: "too_many_attempts", retryAfter: 42 });
  });
  it("keeps field errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(json(422, { error: "invalid_config", message: "bad", fields: { "window.end": "after start" } }));
    const e = await api.patchConfig({}).catch((x) => x);
    expect(e).toBeInstanceOf(ApiError);
    expect(e.fields).toEqual({ "window.end": "after start" });
  });
  it("turns a network failure into NetworkError", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("fail"));
    await expect(api.status()).rejects.toBeInstanceOf(NetworkError);
  });
  it("never saves a JSON error as the CSV file", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(json(400, { error: "bad_range", message: "no" }));
    await expect(api.exportCsv("2026-02-01", "2026-01-01", false)).rejects.toMatchObject({ code: "bad_range" });
  });
  it("takes the file name from Content-Disposition", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("a,b", { status: 200, headers: { "Content-Disposition": 'attachment; filename="attendance_2026-10-01_to_2026-10-31.csv"' } }));
    const r = await api.exportCsv("2026-10-01", "2026-10-31", false);
    expect(r.filename).toBe("attendance_2026-10-01_to_2026-10-31.csv");
    expect(filenameFromDisposition(null)).toBeNull();
  });
});
