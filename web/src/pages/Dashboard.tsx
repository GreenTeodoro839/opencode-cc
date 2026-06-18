import { useEffect, useState } from "react";
import {
  api,
  HourPoint,
  Latency,
  ModelUsagePoint,
  OpenCodeGoQuotaAccount,
  OpenCodeGoQuotaWindow,
  Summary,
} from "../lib/api";
import { fmtMs, fmtNum } from "../lib/format";
import { PageHeader } from "../App";
import { Card, EmptyState, Spinner, StatCard, Badge } from "../components/ui";
import { RequestsAreaChart, TokensAreaChart } from "../components/charts";

export default function Dashboard() {
  const [summary, setSummary] = useState<Summary | null>(null);
  const [hourly, setHourly] = useState<HourPoint[]>([]);
  const [latency, setLatency] = useState<Latency | null>(null);
  const [models, setModels] = useState<ModelUsagePoint[]>([]);
  const [goQuota, setGoQuota] = useState<OpenCodeGoQuotaAccount[]>([]);
  const [goQuotaLoading, setGoQuotaLoading] = useState(false);
  const [goQuotaErr, setGoQuotaErr] = useState("");
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string>("");

  async function refreshQuota() {
    setGoQuotaLoading(true);
    try {
      const quota = await api.opencodeGoQuota();
      setGoQuota(quota);
      setGoQuotaErr("");
    } catch (e) {
      setGoQuotaErr((e as Error).message);
    } finally {
      setGoQuotaLoading(false);
    }
  }

  async function refreshStats() {
    try {
      const [s, h, l, m] = await Promise.all([
        api.summary(),
        api.hourly(24),
        api.latency(),
        api.models(24),
      ]);
      setSummary(s);
      setHourly(h);
      setLatency(l);
      setModels(m);
      setErr("");
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  async function refresh() {
    refreshStats();
    refreshQuota();
  }

  useEffect(() => {
    refreshStats();
    refreshQuota();
    const statsID = setInterval(refreshStats, 8000);
    const quotaID = setInterval(refreshQuota, 60000);
    return () => {
      clearInterval(statsID);
      clearInterval(quotaID);
    };
  }, []);

  if (loading && !summary) {
    return (
      <div className="flex items-center justify-center py-24 text-slate-500 gap-2">
        <Spinner /> loading…
      </div>
    );
  }

  const errorRate = summary?.requests_last_24h
    ? (summary.errors_last_24h / summary.requests_last_24h) * 100
    : 0;
  const inputTokens = summary?.total_input_tokens ?? 0;
  const cachedInputTokens = summary?.total_cached_input_tokens ?? 0;
  const cacheCreationTokens = summary?.total_cache_creation_input_tokens ?? 0;
  const cacheHitRate = inputTokens ? (cachedInputTokens / inputTokens) * 100 : 0;

  const maxModelReq = Math.max(1, ...models.map((m) => m.requests));

  return (
    <div className="animate-fade-in">
      <PageHeader
        title="仪表盘"
        desc="代理实时流量、Token 用量与上游延迟。"
        actions={
          <button onClick={refresh} className="btn-ghost">
            <RefreshIcon /> 刷新
          </button>
        }
      />

      {err && (
        <div className="mb-5 rounded-xl border border-accent-red/20 bg-accent-red/10 px-4 py-3 text-sm text-accent-red">
          无法访问面板接口：{err}
        </div>
      )}

      {/* Stat row */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-4 mb-6">
        <StatCard
          label="请求数 · 24小时"
          value={fmtNum(summary?.requests_last_24h ?? 0)}
          sub={`累计 ${fmtNum(summary?.total_requests ?? 0)} 次`}
          accent="text-white"
        />
        <StatCard
          label="错误率 · 24小时"
          value={`${errorRate.toFixed(1)}%`}
          sub={`${fmtNum(summary?.errors_last_24h ?? 0)} 次错误`}
          accent={errorRate > 5 ? "text-accent-red" : "text-accent-green"}
        />
        <StatCard
          label="Token · 24小时"
          value={fmtNum(
            (summary?.total_input_tokens ?? 0) + (summary?.total_output_tokens ?? 0)
          )}
          sub={`输入 ${fmtNum(summary?.total_input_tokens ?? 0)} · 输出 ${fmtNum(
            summary?.total_output_tokens ?? 0
          )}`}
          accent="text-accent-cyan"
        />
        <StatCard
          label="缓存命中"
          value={`${cacheHitRate.toFixed(1)}%`}
          sub={`命中 ${fmtNum(cachedInputTokens)} · 写入 ${fmtNum(cacheCreationTokens)}`}
          accent={cacheHitRate > 0 ? "text-accent-green" : "text-slate-400"}
        />
        <StatCard
          label="延迟 p95"
          value={latency ? fmtMs(latency.p95) : "—"}
          sub={
            latency
              ? `p50 ${fmtMs(latency.p50)} · p99 ${fmtMs(latency.p99)}`
              : "暂无数据"
          }
          accent="text-accent-glow"
        />
      </div>

      {(goQuota.length > 0 || goQuotaErr) && (
        <Card className="mb-6">
          <div className="flex items-center justify-between gap-3 mb-4">
            <div>
              <h3 className="text-sm font-semibold text-slate-200">OpenCode Go 额度</h3>
              <p className="text-xs text-slate-500 mt-1">
                来自账号 Dashboard 的 5h、周限、月限百分比。
              </p>
            </div>
            <div className="flex items-center gap-2">
              {goQuotaLoading && <Spinner className="text-slate-500" />}
              <Badge tone={goQuota.some((q) => q.success) ? "green" : "amber"}>
                {goQuota.length} 个账号
              </Badge>
            </div>
          </div>

          {goQuotaErr && (
            <div className="mb-3 rounded-xl border border-accent-red/20 bg-accent-red/10 px-3 py-2 text-xs text-accent-red">
              额度接口失败：{goQuotaErr}
            </div>
          )}

          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {goQuota.map((account) => (
              <QuotaAccountCard key={`${account.index}-${account.base_url}`} account={account} />
            ))}
          </div>
        </Card>
      )}

      {/* Charts */}
      <div className="grid grid-cols-1 xl:grid-cols-2 gap-4 mb-6">
        <Card>
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-semibold text-slate-200">每小时请求数</h3>
            <div className="flex gap-2">
              <Badge tone="violet">请求</Badge>
              <Badge tone="red">错误</Badge>
            </div>
          </div>
          {hourly.length > 0 ? (
            <RequestsAreaChart data={hourly} />
          ) : (
            <EmptyState title="暂无流量" hint="通过代理发送一个请求即可填充图表。" />
          )}
        </Card>
        <Card>
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-semibold text-slate-200">每小时 Token</h3>
            <div className="flex gap-2">
              <Badge tone="cyan">输入</Badge>
              <Badge tone="green">输出</Badge>
            </div>
          </div>
          {hourly.length > 0 ? (
            <TokensAreaChart data={hourly} />
          ) : (
            <EmptyState title="暂无 Token" hint="请求完成后将显示 Token 用量。" />
          )}
        </Card>
      </div>

      {/* Model breakdown */}
      <Card>
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold text-slate-200">模型用量 · 24小时</h3>
          <Badge>{models.length} 个模型</Badge>
        </div>
        {models.length === 0 ? (
          <EmptyState title="暂无模型用量" hint="每个解析后的目标模型都会在这里统计。" />
        ) : (
          <div className="space-y-3">
            {models.map((m) => (
              <div key={m.model} className="flex items-center gap-4">
                <div className="w-40 shrink-0 truncate font-mono text-sm text-slate-300">
                  {m.model}
                </div>
                <div className="flex-1 h-7 rounded-lg bg-white/[0.03] overflow-hidden">
                  <div
                    className="h-full bg-gradient-to-r from-accent/70 to-accent-cyan/60 transition-all"
                    style={{ width: `${(m.requests / maxModelReq) * 100}%` }}
                  />
                </div>
                <div className="w-24 text-right text-sm text-slate-400 tabular-nums">
                  {fmtNum(m.requests)} 次
                </div>
                <div className="w-24 text-right text-sm text-slate-500 tabular-nums">
                  {fmtNum(m.tokens)} tok
                </div>
                <div className="w-24 text-right text-xs text-accent-green/80 tabular-nums">
                  {m.cached_input_tokens ? `${fmtNum(m.cached_input_tokens)} 缓存` : "—"}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function QuotaAccountCard({ account }: { account: OpenCodeGoQuotaAccount }) {
  const title = account.name || `账号 #${account.index + 1}`;
  return (
    <div className="rounded-xl border border-white/[0.06] bg-white/[0.03] p-4">
      <div className="flex items-start justify-between gap-3 mb-3">
        <div className="min-w-0">
          <div className="text-sm font-medium text-slate-200 truncate">{title}</div>
          <div className="text-xs text-slate-500 font-mono truncate">{account.base_url}</div>
        </div>
        <Badge tone={account.success ? "green" : "red"}>
          {account.success ? "已更新" : "失败"}
        </Badge>
      </div>

      {!account.success ? (
        <div className="rounded-lg border border-accent-red/20 bg-accent-red/10 px-3 py-2 text-xs text-accent-red">
          {account.error || "额度查询失败"}
        </div>
      ) : (
        <div className="space-y-3">
          {(account.windows || []).map((win) => (
            <QuotaWindowBar key={win.label} win={win} />
          ))}
          {(!account.windows || account.windows.length === 0) && (
            <div className="text-xs text-slate-500">没有启用的额度窗口。</div>
          )}
        </div>
      )}
    </div>
  );
}

function QuotaWindowBar({ win }: { win: OpenCodeGoQuotaWindow }) {
  const used = Math.max(0, Math.min(100, win.used));
  const tone =
    used >= 90
      ? "from-accent-red to-accent-red/70"
      : used >= 70
      ? "from-accent-amber to-accent-amber/70"
      : "from-accent-green to-accent-green/70";
  return (
    <div>
      <div className="flex items-center justify-between gap-3 mb-1.5">
        <span className="text-xs font-medium text-slate-300">{quotaLabel(win.label)}</span>
        <span className={`text-xs font-semibold tabular-nums ${quotaTextColor(used)}`}>
          {used.toFixed(0)}%
        </span>
      </div>
      <div className="h-2 rounded-full bg-white/[0.06] overflow-hidden">
        <div
          className={`h-full rounded-full bg-gradient-to-r ${tone} transition-all`}
          style={{ width: `${used}%` }}
        />
      </div>
      <div className="flex items-center justify-between mt-1 text-[11px] text-slate-500">
        <span>剩余 {win.remaining.toFixed(0)}%</span>
        <span title={new Date(win.reset_at).toLocaleString()}>
          {formatReset(win.reset_at)}
        </span>
      </div>
    </div>
  );
}

function quotaLabel(label: string): string {
  if (label === "5h Rolling") return "5h 限制";
  if (label === "Weekly") return "周限";
  if (label === "Monthly") return "月限";
  return label;
}

function quotaTextColor(used: number): string {
  if (used >= 90) return "text-accent-red";
  if (used >= 70) return "text-accent-amber";
  return "text-accent-green";
}

function formatReset(iso: string): string {
  const diffMs = new Date(iso).getTime() - Date.now();
  if (!Number.isFinite(diffMs) || diffMs <= 0) return "即将重置";
  const totalMinutes = Math.floor(diffMs / 60000);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `重置 ${days}d${hours}h`;
  if (hours > 0) return `重置 ${hours}h${minutes}m`;
  return `重置 ${minutes}m`;
}

function RefreshIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
