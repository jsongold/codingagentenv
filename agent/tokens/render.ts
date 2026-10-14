// render: UnitUsage[] -> text. Appearance only; the numbers are passed through as they are.
import type { UnitUsage } from "./aggregate.ts";
import type { GroupUsage } from "./group.ts";

const COUNTS = ["calls", "input", "output", "cacheRead", "cacheCreation", "total"] as const;
type Totals = Record<(typeof COUNTS)[number], number>;

function totals(units: Pick<UnitUsage, (typeof COUNTS)[number]>[]): Totals {
  const t: Totals = { calls: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, total: 0 };
  for (const u of units) for (const k of COUNTS) t[k] += u[k];
  return t;
}

function table(units: UnitUsage[]): string {
  const row = (lead: string[], n: Totals, description: string): string[] => [
    ...lead,
    ...COUNTS.map((k) => String(n[k])),
    description,
  ];
  const rows = [
    ["unit", "session", "agent", "models", ...COUNTS, "description"],
    ...units.map((u) =>
      row(
        [u.unit, u.session.slice(0, 8), u.agent ?? "", u.models.map((m) => m.replace(/^claude-/, "")).join(",")],
        u,
        u.description ?? "",
      ),
    ),
    row(["total", "", "", ""], totals(units), ""),
  ];
  const width = rows[0].map((_, i) => Math.max(...rows.map((r) => r[i].length)));
  const isCount = (i: number): boolean => i >= 4 && i < 4 + COUNTS.length;
  return rows
    .map((r) => r.map((c, i) => (isCount(i) ? c.padStart(width[i]) : c.padEnd(width[i]))).join("  ").trimEnd())
    .join("\n");
}

export function render(units: UnitUsage[], json: boolean): string {
  return json ? JSON.stringify({ units, total: totals(units) }, null, 1) : table(units);
}

// One row per group, largest first, with the share of the overall total.
export function renderGroups(groups: GroupUsage[], json: boolean): string {
  const sum = totals(groups);
  if (json) return JSON.stringify({ groups, total: sum }, null, 1);
  const share = (n: number): string => (sum.total === 0 ? "0.0%" : `${((100 * n) / sum.total).toFixed(1)}%`);
  const rows = [
    ["group", ...COUNTS, "share"],
    ...groups.map((g) => [g.group, ...COUNTS.map((k) => String(g[k])), share(g.total)]),
    ["total", ...COUNTS.map((k) => String(sum[k])), share(sum.total)],
  ];
  const width = rows[0].map((_, i) => Math.max(...rows.map((r) => r[i].length)));
  return rows
    .map((r) => r.map((c, i) => (i === 0 ? c.padEnd(width[i]) : c.padStart(width[i]))).join("  ").trimEnd())
    .join("\n");
}
