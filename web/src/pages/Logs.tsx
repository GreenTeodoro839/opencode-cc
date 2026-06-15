import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api, LogRow } from "../lib/api";
import { fmtMs, fmtTime, fmtDateTime, prettyJson, relTime, statusTone } from "../lib/format";
import { PageHeader } from "../App";
import { Badge, Card, EmptyState, Spinner } from "../components/ui";

export default function Logs() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [rows, setRows] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState("");

  async function refresh() {
    try {
      setRows(await api.logs(150));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
    const i = setInterval(refresh, 6000);
    return () => clearInterval(i);
  }, []);

  const shown = filter
    ? rows.filter(
        (r) =>
          r.target_model.toLowerCase().includes(filter.toLowerCase()) ||
          r.incoming_model.toLowerCase().includes(filter.toLowerCase()) ||
          String(r.status).includes(filter) ||
          r.error.toLowerCase().includes(filter.toLowerCase())
      )
    : rows;

  return (
    <div className="animate-fade-in">
      <PageHeader
        title="请求日志"
        desc="每个代理请求，含转换前后的完整载荷。"
        actions={
          <input
            className="input w-56"
            placeholder="筛选 模型 / 状态…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        }
      />

      <Card className="overflow-hidden p-0">
        {loading ? (
          <div className="flex items-center justify-center py-16 text-slate-500 gap-2">
            <Spinner /> 加载中…
          </div>
        ) : shown.length === 0 ? (
          <EmptyState title="暂无请求" hint="转发到 Zen 的请求会实时显示在这里。" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-white/[0.05]">
                  <th className="font-medium px-4 py-3">时间</th>
                  <th className="font-medium px-4 py-3">模型 → 目标</th>
                  <th className="font-medium px-4 py-3">状态</th>
                  <th className="font-medium px-4 py-3">延迟</th>
                  <th className="font-medium px-4 py-3">Token</th>
                  <th className="font-medium px-4 py-3">停止原因</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((r) => {
                  const active = id && Number(id) === r.id;
                  return (
                    <tr
                      key={r.id}
                      onClick={() => navigate(`/logs/${r.id}`)}
                      className={`border-b border-white/[0.03] cursor-pointer transition-colors ${
                        active ? "bg-accent/10" : "hover:bg-white/[0.02]"
                      }`}
                    >
                      <td className="px-4 py-3 whitespace-nowrap">
                        <div className="text-slate-300 font-mono text-xs">{fmtTime(r.ts)}</div>
                        <div className="text-[10px] text-slate-600">{relTime(r.ts)}</div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-xs text-slate-400">{r.incoming_model}</span>
                          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-slate-600">
                            <path d="M5 12h14M13 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                          <span className="font-mono text-xs text-accent-glow">{r.target_model}</span>
                          {r.stream && <Badge tone="cyan">流式</Badge>}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <span className={`font-mono font-medium ${statusTone(r.status)}`}>
                          {r.status || "—"}
                        </span>
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-slate-400 tabular-nums">
                        {r.duration_ms ? fmtMs(r.duration_ms) : "—"}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-slate-400 tabular-nums">
                        {r.input_tokens || r.output_tokens ? (
                          <>
                            <span className="text-accent-cyan">{r.input_tokens}</span>
                            <span className="text-slate-600 mx-1">/</span>
                            <span className="text-accent-green">{r.output_tokens}</span>
                          </>
                        ) : (
                          "—"
                        )}
                      </td>
                      <td className="px-4 py-3">
                        {r.error ? (
                          <Badge tone="red" >
                            <span className="truncate max-w-[180px] inline-block align-bottom">{r.error}</span>
                          </Badge>
                        ) : r.stop_reason ? (
                          <Badge tone={r.stop_reason === "error" ? "red" : "default"}>{r.stop_reason}</Badge>
                        ) : (
                          <span className="text-slate-600">—</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {id && <DetailDrawer id={Number(id)} onClose={() => navigate("/logs")} />}
    </div>
  );
}

function DetailDrawer({ id, onClose }: { id: number; onClose: () => void }) {
  const [row, setRow] = useState<LogRow | null>(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<"req" | "resp">("req");

  useEffect(() => {
    setLoading(true);
    api
      .log(id)
      .then(setRow)
      .finally(() => setLoading(false));
  }, [id]);

  return (
    <div className="fixed inset-0 z-40 flex justify-end">
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm animate-fade-in"
        onClick={onClose}
      />
      <div className="relative w-full max-w-2xl h-full glass rounded-none border-l border-y-0 border-r-0 overflow-y-auto animate-fade-in">
        <div className="sticky top-0 z-10 bg-ink-850/90 backdrop-blur-xl border-b border-white/[0.06] px-6 py-4 flex items-center justify-between">
          <div>
            <div className="text-sm font-semibold text-white">请求 #{id}</div>
            {row && (
              <div className="text-xs text-slate-500">{fmtDateTime(row.ts)}</div>
            )}
          </div>
          <button onClick={onClose} className="btn-ghost !px-2.5 !py-2">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M18 6L6 18M6 6l12 12" strokeLinecap="round" />
            </svg>
          </button>
        </div>

        <div className="p-6">
          {loading ? (
            <div className="flex items-center justify-center py-16 text-slate-500 gap-2">
              <Spinner /> 加载中…
            </div>
          ) : !row ? (
            <EmptyState title="未找到" />
          ) : (
            <>
              <div className="grid grid-cols-2 gap-3 mb-5">
                <Field label="来源模型" value={row.incoming_model} mono />
                <Field label="目标模型" value={row.target_model} mono accent />
                <Field label="状态码" value={String(row.status)} mono tone={statusTone(row.status)} />
                <Field label="延迟" value={row.duration_ms ? fmtMs(row.duration_ms) : "—"} mono />
                <Field label="输入 Token" value={String(row.input_tokens)} mono />
                <Field label="输出 Token" value={String(row.output_tokens)} mono />
                <Field label="停止原因" value={row.stop_reason || "—"} mono />
                <Field label="流式" value={row.stream ? "是" : "否"} mono />
              </div>

              {row.error && (
                <div className="mb-5 rounded-xl border border-accent-red/20 bg-accent-red/10 px-4 py-3 text-sm text-accent-red font-mono">
                  {row.error}
                </div>
              )}

              <div className="flex gap-1 mb-3 bg-ink-900/60 p-1 rounded-xl w-fit">
                <TabBtn active={tab === "req"} onClick={() => setTab("req")}>
                  请求
                </TabBtn>
                <TabBtn active={tab === "resp"} onClick={() => setTab("resp")}>
                  响应
                </TabBtn>
              </div>
              <CodeBlock>
                {prettyJson(tab === "req" ? row.req_body || "" : row.resp_body || "") ||
                  "（空）"}
              </CodeBlock>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function Field({
  label,
  value,
  mono,
  accent,
  tone,
}: {
  label: string;
  value: string;
  mono?: boolean;
  accent?: boolean;
  tone?: string;
}) {
  return (
    <div className="rounded-xl bg-white/[0.03] border border-white/[0.05] px-3 py-2">
      <div className="text-[10px] uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-sm ${mono ? "font-mono" : ""} ${accent ? "text-accent-glow" : tone || "text-slate-200"}`}>
        {value}
      </div>
    </div>
  );
}

function TabBtn({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1.5 text-xs font-medium rounded-lg transition ${
        active ? "bg-white/[0.08] text-white" : "text-slate-400 hover:text-slate-200"
      }`}
    >
      {children}
    </button>
  );
}

function CodeBlock({ children }: { children: React.ReactNode }) {
  return (
    <pre className="rounded-xl bg-ink-950/80 border border-white/[0.05] p-4 text-xs font-mono text-slate-300 overflow-auto max-h-[40vh] leading-relaxed">
      <code>{children}</code>
    </pre>
  );
}
