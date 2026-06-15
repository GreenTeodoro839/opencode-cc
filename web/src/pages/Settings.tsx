import { useEffect, useState } from "react";
import { api, PanelConfig } from "../lib/api";
import { PageHeader } from "../App";
import { Badge, Card, Spinner } from "../components/ui";

export default function Settings() {
  const [cfg, setCfg] = useState<PanelConfig | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [upstream, setUpstream] = useState("https://opencode.ai/zen");
  const [logReqs, setLogReqs] = useState(true);
  const [maxBody, setMaxBody] = useState(16384);
  const [timeout, setTimeoutSecs] = useState(0);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [flash, setFlash] = useState("");

  useEffect(() => {
    api.getConfig().then((c) => {
      setCfg(c);
      setUpstream(c.upstream_base);
      setLogReqs(c.log_requests);
      setMaxBody(c.max_body_log_bytes);
      setTimeoutSecs(c.request_timeout_seconds);
    });
  }, []);

  async function save() {
    setSaving(true);
    try {
      const body: Record<string, unknown> = {
        upstream_base: upstream,
        log_requests: logReqs,
        max_body_log_bytes: maxBody,
        request_timeout_seconds: timeout,
      };
      // Only send the API key if the user typed a new one.
      if (apiKey.trim()) body.zen_api_key = apiKey.trim();
      const updated = await api.putConfig(body);
      setCfg(updated);
      setApiKey("");
      setDirty(false);
      setFlash("已保存。");
      setTimeout(() => setFlash(""), 2500);
    } finally {
      setSaving(false);
    }
  }

  if (!cfg) {
    return (
      <div className="flex items-center justify-center py-24 text-slate-500 gap-2">
        <Spinner /> 加载中…
      </div>
    );
  }

  const keyMasked = cfg.zen_api_key_set ? cfg.zen_api_key_masked : "not set";

  return (
    <div className="animate-fade-in max-w-3xl">
      <PageHeader
        title="设置"
        desc="上游凭据与代理行为。"
        actions={
          <div className="flex items-center gap-3">
            {flash && <span className="text-xs text-accent-green">{flash}</span>}
            <button onClick={save} disabled={saving || !dirty} className="btn-primary">
              {saving ? <Spinner /> : <SaveIcon />}
              保存
            </button>
          </div>
        }
      />

      <Card className="mb-4">
        <div className="flex items-center gap-2 mb-4">
          <KeyIcon />
          <h3 className="text-sm font-semibold text-slate-200">OpenCode Zen 凭据</h3>
          {cfg.zen_api_key_set ? <Badge tone="green">已配置</Badge> : <Badge tone="amber">必填</Badge>}
        </div>

        <label className="label">API Key</label>
        <div className="flex gap-2 mb-1">
          <input
            type="password"
            className="input font-mono"
            placeholder={cfg.zen_api_key_set ? `当前：${keyMasked}` : "粘贴你的 Zen API Key"}
            value={apiKey}
            onChange={(e) => {
              setApiKey(e.target.value);
              setDirty(true);
            }}
          />
        </div>
        <p className="text-xs text-slate-500 mb-4">
          从 <span className="font-mono text-slate-400">opencode.ai/zen</span> 获取。仅本地保存在
          <span className="font-mono text-slate-400"> data/config.json</span>，除转发给 Zen 外不会发送到任何地方。
        </p>

        <label className="label">上游 Base URL</label>
        <input
          className="input font-mono"
          value={upstream}
          onChange={(e) => {
            setUpstream(e.target.value);
            setDirty(true);
          }}
        />
        <p className="text-xs text-slate-500 mt-2">
          请求会发送到 <span className="font-mono text-slate-400">{upstream.replace(/\/$/, "")}/v1/chat/completions</span>。
        </p>
      </Card>

      <Card className="mb-4">
        <div className="flex items-center gap-2 mb-4">
          <SlidersIcon />
          <h3 className="text-sm font-semibold text-slate-200">行为</h3>
        </div>

        <Toggle
          label="记录请求"
          desc="为面板记录每个代理请求及其转换后的响应。"
          checked={logReqs}
          onChange={(v) => {
            setLogReqs(v);
            setDirty(true);
          }}
        />

        <div className="grid grid-cols-2 gap-4 mt-4">
          <div>
            <label className="label">日志最大体积（字节）</label>
            <input
              type="number"
              className="input font-mono"
              value={maxBody}
              min={0}
              onChange={(e) => {
                setMaxBody(Number(e.target.value));
                setDirty(true);
              }}
            />
          </div>
          <div>
            <label className="label">上游超时（秒，0 = 不限时）</label>
            <input
              type="number"
              className="input font-mono"
              value={timeout}
              min={0}
              onChange={(e) => {
                setTimeoutSecs(Number(e.target.value));
                setDirty(true);
              }}
            />
          </div>
        </div>
      </Card>

      <Card>
        <div className="flex items-center gap-2 mb-4">
          <TerminalIcon />
          <h3 className="text-sm font-semibold text-slate-200">接入 Claude Code</h3>
        </div>
        <p className="text-sm text-slate-400 mb-3">
          通过两个环境变量把 Claude Code 指向本代理：
        </p>
        <pre className="rounded-xl bg-ink-950/80 border border-white/[0.05] p-4 text-xs font-mono text-slate-300 overflow-x-auto">
{`# Bash / zsh
export ANTHROPIC_BASE_URL=http://localhost:${cfg.listen_addr.split(":").pop() || "8787"}
export ANTHROPIC_AUTH_TOKEN=anything
claude

# PowerShell
$env:ANTHROPIC_BASE_URL="http://localhost:${cfg.listen_addr.split(":").pop() || "8787"}"
$env:ANTHROPIC_AUTH_TOKEN="anything"
claude`}
        </pre>
        <p className="text-xs text-slate-500 mt-3">
          AUTH_TOKEN 的值无关紧要——代理始终用你的 Zen Key 向上游鉴权。仅当你把这个端口暴露到本机以外时，才需要在
          <span className="text-slate-400"> config.json</span> 中设置
          <span className="text-slate-400"> 面板访问令牌</span>。
        </p>
      </Card>
    </div>
  );
}

function Toggle({
  label,
  desc,
  checked,
  onChange,
}: {
  label: string;
  desc: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-start justify-between gap-4 cursor-pointer">
      <div>
        <div className="text-sm text-slate-200">{label}</div>
        <div className="text-xs text-slate-500">{desc}</div>
      </div>
      <button
        type="button"
        onClick={() => onChange(!checked)}
        className={`relative w-11 h-6 rounded-full transition-colors shrink-0 mt-0.5 ${
          checked ? "bg-accent" : "bg-ink-600"
        }`}
      >
        <span
          className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${
            checked ? "translate-x-5" : ""
          }`}
        />
      </button>
    </label>
  );
}

function SaveIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" strokeLinejoin="round" />
      <path d="M17 21v-8H7v8M7 3v5h8" strokeLinejoin="round" />
    </svg>
  );
}
function KeyIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-slate-500">
      <path d="M21 2l-2 2m-7.6 7.6a5 5 0 1 1-7.07 7.07 5 5 0 0 1 7.07-7.07zm0 0L15 8m0 0l3 3 3-3-3-3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function SlidersIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-slate-500">
      <path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6" strokeLinecap="round" />
    </svg>
  );
}
function TerminalIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-slate-500">
      <path d="M4 17l6-6-6-6M12 19h8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
