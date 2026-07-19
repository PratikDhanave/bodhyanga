// ConfidenceGauge — the console's signature element. A precise threshold meter
// that carries the "AI diagnostic trust" thesis: the reviewer sees at a glance
// where the model's confidence falls against the clinical decision bands.
// Thresholds: <50 Low, 50–70 Moderate, 70–90 High, 90+ Very high.

interface Band {
  label: string;
  color: string;
}

function band(pct: number): Band {
  if (pct < 50) return { label: "Low", color: "#b42318" };
  if (pct < 70) return { label: "Moderate", color: "#92600a" };
  if (pct < 90) return { label: "High", color: "#0d7378" };
  return { label: "Very high", color: "#067647" };
}

const TICKS = [50, 70, 90];

export function ConfidenceGauge({
  value,
  size = "md",
}: {
  value: number;
  size?: "sm" | "md";
}) {
  const pct = Math.round(Math.max(0, Math.min(1, value)) * 100);
  const b = band(pct);
  const trackH = size === "sm" ? 6 : 10;

  return (
    <div
      className="flex items-center gap-3"
      role="meter"
      aria-valuenow={pct}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={`AI confidence ${pct} percent, ${b.label}`}
    >
      <div className="relative flex-1" style={{ minWidth: size === "sm" ? 90 : 140 }}>
        <div
          className="w-full overflow-hidden rounded-full bg-ink-100"
          style={{ height: trackH }}
        >
          <div
            className="h-full rounded-full transition-[width] duration-500"
            style={{ width: `${pct}%`, background: b.color }}
          />
        </div>
        {/* Decision-band threshold ticks */}
        {TICKS.map((t) => (
          <span
            key={t}
            className="absolute top-1/2 w-px -translate-y-1/2 bg-ink-300"
            style={{ left: `${t}%`, height: trackH + 4 }}
            aria-hidden
          />
        ))}
      </div>
      <div className="flex shrink-0 items-baseline gap-1.5">
        <span
          className="tnum font-semibold"
          style={{ color: b.color, fontSize: size === "sm" ? "0.875rem" : "1.05rem" }}
        >
          {pct}%
        </span>
        {size === "md" && (
          <span className="text-xs font-medium text-ink-400">{b.label}</span>
        )}
      </div>
    </div>
  );
}
