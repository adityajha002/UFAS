import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { api, ApiError, setToken } from "../api";
import { AttendancePanel } from "../panels/AttendancePanel";
import { ExitRequestModal } from "../components/dialogs";
import { Ctx, type AppCtx } from "../ctx";
import type { Day, ExitRequestDetail, Status } from "../types";

const mockStatus: Status = {
  running: true,
  started_at: "2026-11-10 09:00:00",
  uptime_s: 3600,
  server_time_ist: "2026-11-10 12:00:00",
  public_listener: { configured: "0.0.0.0:8080", reachable: true, loopback_only: false },
  admin_listener: { configured: "127.0.0.1:9090" },
  network: { hostname: "host", addresses: [], phone_urls: [], warning: "" },
  attendance_window: {
    open: true,
    reason: "open",
    start: "09:00",
    end: "17:00",
    next_change_at_ist: null,
    weekday: "Tuesday",
  },
  database: { file: "attendance.db", size_bytes: 1000, ok: true },
  counts: { faculty: 2, enrolled: 2, marked_today: 1 },
  attendance_attempts_24h: { ok: 1, failed: 0 },
  config_warnings: [],
};

function renderWithCtx(ui: React.ReactNode, overrides: Partial<AppCtx> = {}) {
  const ctx: AppCtx = {
    status: mockStatus,
    today: "2026-11-10",
    down: false,
    stopped: false,
    users: [],
    version: 0,
    notify: vi.fn(),
    toast: vi.fn(),
    openModal: vi.fn(),
    closeModal: vi.fn(),
    markStopping: vi.fn(),
    auditPreset: null,
    requestAuditFilter: vi.fn(),
    ...overrides,
  };
  return {
    ...render(<Ctx.Provider value={ctx}>{ui}</Ctx.Provider>),
    ctx,
  };
}

const mockDay: Day = {
  date: "2026-11-10",
  is_today: true,
  is_weekend: false,
  is_future: false,
  summary: { total: 2, marked: 2, not_marked: 0, not_enrolled: 0 },
  faculty: [
    {
      employee_id: "EMP001",
      name: "Dr. Alice Smith",
      enrolled: true,
      marked: true,
      marked_at_ist: "2026-11-10 09:15:00",
      distance_m: 20,
      source: "phone",
      note: "",
      pending_exit_request: {
        id: "req-123",
        requested_at_ist: "2026-11-10 11:30:00",
        status: "pending",
      },
    },
    {
      employee_id: "EMP002",
      name: "Prof. Bob Jones",
      enrolled: true,
      marked: true,
      marked_at_ist: "2026-11-10 09:30:00",
      distance_m: 15,
      source: "phone",
      note: "",
      pending_exit_request: null,
    },
  ],
};

const mockDetail: ExitRequestDetail = {
  id: "req-123",
  employee_id: "EMP001",
  name: "Dr. Alice Smith",
  day: "2026-11-10",
  requested_at: 1762754400,
  requested_at_ist: "2026-11-10 11:30:00",
  lat_micro: 28606827,
  lon_micro: 77024455,
  accuracy_cm: 1200,
  distance_m: 20,
  status: "pending",
  is_in: true,
};

describe("Exit Requests API Client", () => {
  beforeEach(() => {
    setToken("secret-token");
    vi.restoreAllMocks();
  });

  const jsonResponse = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });

  it("calls list endpoint with filters", async () => {
    const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { items: [mockDetail] }));
    const res = await api.exitRequests({ date: "2026-11-10", status: "pending" });
    expect(res.items).toHaveLength(1);
    expect(f).toHaveBeenCalledWith(
      "/admin/api/exit-requests?date=2026-11-10&status=pending",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("calls detail endpoint", async () => {
    const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, mockDetail));
    const res = await api.exitRequest("req-123");
    expect(res.id).toBe("req-123");
    expect(f).toHaveBeenCalledWith(
      "/admin/api/exit-requests/req-123",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("calls approve endpoint", async () => {
    const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, {
      id: "req-123",
      employee_id: "EMP001",
      status: "approved",
      approved_at_ist: "2026-11-10 11:45:00",
      out_at_ist: "2026-11-10 11:45:00",
      session_s: 9000,
      today_worked_s: 9000,
      is_in: false,
    }));
    const res = await api.approveExitRequest("req-123");
    expect(res.status).toBe("approved");
    expect(f).toHaveBeenCalledWith(
      "/admin/api/exit-requests/req-123/approve",
      expect.objectContaining({ method: "POST" })
    );
  });
});

describe("AttendancePanel Pending Exit Request Highlights", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("highlights pending request in yellow with label and opens modal on click", async () => {
    vi.spyOn(api, "day").mockResolvedValue(mockDay);
    const { ctx } = renderWithCtx(<AttendancePanel selected={null} onBack={vi.fn()} />);

    // Wait for day data to render
    await waitFor(() => {
      expect(screen.getByText("Dr. Alice Smith")).toBeTruthy();
    });

    // Check yellow highlight and chip
    const aliceRow = screen.getByText("Dr. Alice Smith").closest("li");
    expect(aliceRow?.classList.contains("pending-exit")).toBe(true);
    expect(screen.getByText("Early exit requested")).toBeTruthy();

    // Check normal row is not highlighted
    const bobRow = screen.getByText("Prof. Bob Jones").closest("li");
    expect(bobRow?.classList.contains("pending-exit")).toBe(false);
    expect(bobRow?.classList.contains("pill-row")).toBe(true);

    // Click pending row opens exit request modal
    fireEvent.click(aliceRow!);
    expect(ctx.openModal).toHaveBeenCalledWith({
      kind: "exit_request",
      request_id: "req-123",
      employee_id: "EMP001",
      name: "Dr. Alice Smith",
    });
  });

  it("does not highlight approved requests or requests on historical past dates", async () => {
    // Today with approved request (status: approved)
    const todayApproved: Day = {
      ...mockDay,
      faculty: [
        {
          ...mockDay.faculty[0],
          pending_exit_request: {
            id: "req-123",
            requested_at_ist: "2026-11-10 11:30:00",
            status: "approved",
          },
        },
      ],
    };
    vi.spyOn(api, "day").mockResolvedValue(todayApproved);

    renderWithCtx(<AttendancePanel selected={null} onBack={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText("Dr. Alice Smith")).toBeTruthy();
    });

    const aliceRow = screen.getByText("Dr. Alice Smith").closest("li");
    expect(aliceRow?.classList.contains("pending-exit")).toBe(false);
    expect(screen.queryByText("Early exit requested")).toBeNull();

    // Historical date with pending request
    const historicalDay: Day = {
      ...mockDay,
      date: "2026-11-09",
      is_today: false,
    };
    vi.spyOn(api, "day").mockImplementation(async (d?: string) => {
      if (d === "2026-11-09") return historicalDay;
      return mockDay;
    });

    // Click previous day
    const prevBtn = screen.getByRole("button", { name: /previous day/i });
    fireEvent.click(prevBtn);

    await waitFor(() => {
      expect(screen.getByText("Mon 9 Nov 2026")).toBeTruthy();
    });

    const histAliceRow = screen.getByText("Dr. Alice Smith").closest("li");
    expect(histAliceRow?.classList.contains("pending-exit")).toBe(false);
    expect(screen.queryByText("Early exit requested")).toBeNull();
  });
});

describe("ExitRequestModal Details and Approval", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("displays request details and allows admin to approve", async () => {
    vi.spyOn(api, "exitRequest").mockResolvedValue(mockDetail);
    const approveSpy = vi.spyOn(api, "approveExitRequest").mockResolvedValue({
      id: "req-123",
      employee_id: "EMP001",
      status: "approved",
      approved_at_ist: "2026-11-10 11:45:00",
      out_at_ist: "2026-11-10 11:45:00",
      session_s: 9000,
      today_worked_s: 9000,
      is_in: false,
    });

    const { ctx } = renderWithCtx(
      <ExitRequestModal requestId="req-123" employeeId="EMP001" name="Dr. Alice Smith" />
    );

    // Shows details
    await waitFor(() => {
      expect(screen.getByText("Dr. Alice Smith")).toBeTruthy();
      expect(screen.getByText("(EMP001)")).toBeTruthy();
      expect(screen.getByText("Currently IN")).toBeTruthy();
      expect(screen.getByText("Pending")).toBeTruthy();
      expect(screen.getByText("20 m from center")).toBeTruthy();
      expect(screen.getByText("12 m (1200 cm)")).toBeTruthy();
    });

    // Check confirmation prompt and Allow exit button
    expect(screen.getByText("Allow this faculty member to mark OUT now?")).toBeTruthy();
    const allowBtn = screen.getByRole("button", { name: /allow exit/i });
    expect(allowBtn).toBeTruthy();

    // Click Allow exit
    fireEvent.click(allowBtn);

    await waitFor(() => {
      expect(approveSpy).toHaveBeenCalledWith("req-123");
      expect(ctx.toast).toHaveBeenCalledWith("Allowed early exit for Dr. Alice Smith");
      expect(ctx.notify).toHaveBeenCalled();
      expect(ctx.closeModal).toHaveBeenCalled();
    });
  });

  it("does not show Allow Exit button for already approved requests", async () => {
    vi.spyOn(api, "exitRequest").mockResolvedValue({
      ...mockDetail,
      status: "approved",
      approved_at_ist: "2026-11-10 11:45:00",
      approved_by: "admin@127.0.0.1",
      is_in: false,
    });

    renderWithCtx(
      <ExitRequestModal requestId="req-123" employeeId="EMP001" name="Dr. Alice Smith" />
    );

    await waitFor(() => {
      expect(screen.getByText("Approved")).toBeTruthy();
      expect(screen.getByText("OUT")).toBeTruthy();
      expect(screen.getByText("admin@127.0.0.1")).toBeTruthy();
    });

    expect(screen.queryByRole("button", { name: /allow exit/i })).toBeNull();
    const closeButtons = screen.getAllByRole("button", { name: /close/i });
    expect(closeButtons.length).toBeGreaterThan(0);
  });

  it("handles approval error and keeps modal open", async () => {
    vi.spyOn(api, "exitRequest").mockResolvedValue(mockDetail);
    vi.spyOn(api, "approveExitRequest").mockRejectedValue(
      new ApiError(409, "not_in", "faculty member is no longer IN")
    );

    const { ctx } = renderWithCtx(
      <ExitRequestModal requestId="req-123" employeeId="EMP001" name="Dr. Alice Smith" />
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /allow exit/i })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: /allow exit/i }));

    await waitFor(() => {
      expect(screen.getByText("faculty member is no longer IN")).toBeTruthy();
      expect(ctx.closeModal).not.toHaveBeenCalled();
      expect(ctx.notify).toHaveBeenCalled();
    });
  });
});
