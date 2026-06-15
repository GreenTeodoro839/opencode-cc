import { useEffect, useState } from "react";
import {
  api,
  HourPoint,
  Latency,
  ModelUsagePoint,
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
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string>("");

  async function refresh() {
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

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 8000);
    return () => clearInterval(id);
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
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
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
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function RefreshIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
