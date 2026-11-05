import { createContext, useContext } from "react";
import { ApiError, NetworkError } from "./api";
import type { FacultyRow, Modal, Status } from "./types";

export type AuditPreset = { outcome: string; nonce: number } | null;

export type AppCtx = {
  status: Status;
  today: string;            // server date (IST), YYYY-MM-DD
  down: boolean;
  stopped: boolean;         // shutdown was requested from this panel
  users: FacultyRow[];
  version: number;          // bumped after any mutation: panels refetch
  notify: () => void;
  toast: (msg: string, kind?: "ok" | "error") => void;
  openModal: (m: Modal) => void;
  closeModal: () => void;
  markStopping: () => void;
  auditPreset: AuditPreset;
  requestAuditFilter: (outcome: string) => void;
};

export const Ctx = createContext<AppCtx>(null as unknown as AppCtx);
export const useApp = () => useContext(Ctx);

export function errText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof NetworkError) return e.message;
  return "Something went wrong";
}
