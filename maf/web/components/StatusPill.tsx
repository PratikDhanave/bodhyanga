import { type CaseStatus, STATUS_LABEL } from "@/lib/api";

const CLASS: Record<CaseStatus, string> = {
  running: "pill-running",
  awaiting_review: "pill-review",
  completed: "pill-completed",
  rejected: "pill-rejected",
  denied: "pill-denied",
  failed: "pill-failed",
};

export function StatusPill({ status }: { status: CaseStatus }) {
  return <span className={`pill ${CLASS[status]}`}>{STATUS_LABEL[status]}</span>;
}
