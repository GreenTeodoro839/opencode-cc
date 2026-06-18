import { useEffect, useState } from "react";
import { api, PanelConfig, UpstreamView } from "../lib/api";
import { PageHeader } from "../App";
import { Badge, Card, Spinner } from "../components/ui";

// One editable upstream row. api_key is the local edit buffer (empty = keep
// existing, matches the backend "empty = don't change" sentinel).
interface UpstreamRow {
  base_url: string;
  api_key: string;
  name: string;
  enabled: boolean;
  api_key_set: boolean;
  api_key_masked: string;
  opencode_go_workspace_id: string;
  opencode_go_auth_cookie: string;
  opencode_go_auth_cookie_set: boolean;
  opencode_go_auth_cookie_masked: string;
  opencode_go_show_rolling: boolean;
  opencode_go_show_weekly: boolean;
  opencode_go_show_monthly: boolean;
}

const ZEN_BASES = [
  "https://opencode.ai/zen/go",
  "https://opencode.ai/zen/",
];
const DEFAULT_OPENCODE_GO_WORKSPACE = "Default";

export default function Settings() {
  const [cfg, setCfg] = useState<PanelConfig | null>(null);
  const [upstreams, setUpstreams] = useState<UpstreamRow[]>([]);
  const [nativeAnthropic, setNativeAnthropic] = useState(false);
  const [logReqs, setLogReqs] = useState(true);
  const [maxBody, setMaxBody] = useState(16384);
  const [timeout, setTimeoutSecs] = useState(0);
  const [requireKey, setRequireKey] = useState(false);
  const [promptCache, setPromptCache] = useState(true);
  const [promptCacheKeyPrefix, setPromptCacheKeyPrefix] = useState("opencode-cc");
  const [promptCacheAnthropicControl, setPromptCacheAnthropicControl] = useState(true);
  const [promptCacheNormalize, setPromptCacheNormalize] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [flash, setFlash] = useState("");

  // Panel password state
  const [newPanelToken, setNewPanelToken] = useState("");
  const [confirmPanelToken, setConfirmPanelToken] = useState("");
  const [panelTokenError, setPanelTokenError] = useState("");
  const [panelTokenFlash, setPanelTokenFlash] = useState("");

  useEffect(() => {
    api.getConfig().then((c) => {
      setCfg(c);
      setNativeAnthropic(c.native_anthropic);
      setLogReqs(c.log_requests);
      setMaxBody(c.max_body_log_bytes);
      setTimeoutSecs(c.request_timeout_seconds);
      setRequireKey(c.require_api_key);
      setPromptCache(c.prompt_cache_enabled);
      setPromptCacheKeyPrefix(c.prompt_cache_key_prefix || "opencode-cc");
      setPromptCacheAnthropicControl(c.prompt_cache_anthropic_control);
      setPromptCacheNormalize(c.prompt_cache_normalize);
      // Seed the upstreams editor from the server view.
      const rows: UpstreamRow[] = (c.upstreams && c.upstreams.length ? c.upstreams : []).map((u) => ({
        base_url: u.base_url,
        api_key: "",
        name: u.name,
        enabled: u.enabled,
        api_key_set: u.api_key_set,
        api_key_masked: u.api_key_masked,
        opencode_go_workspace_id: u.opencode_go_workspace_id || DEFAULT_OPENCODE_GO_WORKSPACE,
        opencode_go_auth_cookie: "",
        opencode_go_auth_cookie_set: Boolean(u.opencode_go_auth_cookie_set),
        opencode_go_auth_cookie_masked: u.opencode_go_auth_cookie_masked || "",
        opencode_go_show_rolling: u.opencode_go_show_rolling !== false,
        opencode_go_show_weekly: u.opencode_go_show_weekly !== false,
        opencode_go_show_monthly: u.opencode_go_show_monthly !== false,
      }));
      setUpstreams(rows);
    });
  }, []);

  async function savePanelToken() {
    setPanelTokenError("");
    if (newPanelToken !== confirmPanelToken) {
      setPanelTokenError("两次输入的密码不一致");
      return;
    }
    setSaving(true);
    try {
      const updated = await api.putConfig({ panel_token: newPanelToken });
      setCfg(updated);
      setNewPanelToken("");
      setConfirmPanelToken("");
      setPanelTokenFlash(newPanelToken === "" ? "面板密码已清除。" : "面板密码已更新。");
      setTimeout(() => setPanelTokenFlash(""), 2500);
      // If a password was just set, reload so AuthGuard picks up the new state.
      if (newPanelToken !== "") setTimeout(() => window.location.reload(), 1000);
    } finally {
      setSaving(false);
    }
  }

  async function save() {
    setSaving(true);
    try {
      const body: Record<string, unknown> = {
        native_anthropic: nativeAnthropic,
        log_requests: logReqs,
        max_body_log_bytes: maxBody,
        request_timeout_seconds: timeout,
        require_api_key: requireKey,
        prompt_cache_enabled: promptCache,
        prompt_cache_key_prefix: promptCacheKeyPrefix,
        prompt_cache_anthropic_control: promptCacheAnthropicControl,
        prompt_cache_normalize: promptCacheNormalize,
        upstreams: upstreams.map((u) => ({
          base_url: u.base_url,
          // empty api_key = keep existing (backend sentinel); only send typed value
          api_key: u.api_key.trim(),
          name: u.name,
          enabled: u.enabled,
          opencode_go_workspace_id: u.opencode_go_workspace_id.trim() || DEFAULT_OPENCODE_GO_WORKSPACE,
          // empty cookie = keep existing (backend sentinel); only send typed value
          opencode_go_auth_cookie: u.opencode_go_auth_cookie.trim(),
          opencode_go_show_rolling: u.opencode_go_show_rolling,
          opencode_go_show_weekly: u.opencode_go_show_weekly,
          opencode_go_show_monthly: u.opencode_go_show_monthly,
        })),
      };
      const updated = await api.putConfig(body);
      setCfg(updated);
      // Refresh masked key views from the server response.
      if (updated.upstreams) {
        setUpstreams(
          updated.upstreams.map((u) => ({
            base_url: u.base_url,
            api_key: "",
            name: u.name,
            enabled: u.enabled,
            api_key_set: u.api_key_set,
            api_key_masked: u.api_key_masked,
            opencode_go_workspace_id: u.opencode_go_workspace_id || DEFAULT_OPENCODE_GO_WORKSPACE,
            opencode_go_auth_cookie: "",
            opencode_go_auth_cookie_set: Boolean(u.opencode_go_auth_cookie_set),
            opencode_go_auth_cookie_masked: u.opencode_go_auth_cookie_masked || "",
            opencode_go_show_rolling: u.opencode_go_show_rolling !== false,
            opencode_go_show_weekly: u.opencode_go_show_weekly !== false,
            opencode_go_show_monthly: u.opencode_go_show_monthly !== false,
          }))
        );
      }
      setDirty(false);
      setFlash("已保存。");
      setTimeout(() => setFlash(""), 2500);
    } finally {
      setSaving(false);
    }
  }

  // Upstream editor helpers
  function updateUpstream(i: number, patch: Partial<UpstreamRow>) {
    setUpstreams((prev) => prev.map((u, idx) => (idx === i ? { ...u, ...patch } : u)));
    setDirty(true);
  }
  function addUpstream() {
    setUpstreams((prev) => [
      ...prev,
      {
        base_url: ZEN_BASES[0],
        api_key: "",
        name: "",
        enabled: true,
        api_key_set: false,
        api_key_masked: "",
        opencode_go_workspace_id: DEFAULT_OPENCODE_GO_WORKSPACE,
        opencode_go_auth_cookie: "",
        opencode_go_auth_cookie_set: false,
        opencode_go_auth_cookie_masked: "",
        opencode_go_show_rolling: true,
        opencode_go_show_weekly: true,
        opencode_go_show_monthly: true,
      },
    ]);
    setDirty(true);
  }
  function removeUpstream(i: number) {
    setUpstreams((prev) => prev.filter((_, idx) => idx !== i));
    setDirty(true);
  }

  if (!cfg) {
    return (
      <div className="flex items-center justify-center py-24 text-slate-500 gap-2">
        <Spinner /> 加载中…
      </div>
    );
  }

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
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <KeyIcon />
            <h3 className="text-sm font-semibold text-slate-200">上游凭据（轮询）</h3>
            {upstreams.filter((u) => u.enabled && (u.api_key_set || u.api_key.trim())).length > 0 ? (
              <Badge tone="green">{upstreams.filter((u) => u.enabled && (u.api_key_set || u.api_key.trim())).length} 个可用</Badge>
            ) : (
              <Badge tone="amber">未配置</Badge>
            )}
          </div>
          <button onClick={addUpstream} className="btn-ghost !py-1.5 !text-xs">
            + 添加上游
          </button>
        </div>

        <p className="text-xs text-slate-500 mb-4">
          支持多个上游 API Key 按请求轮询。Base URL 可从下拉选预设（
          <span className="font-mono text-slate-400">/zen/go</span> go 套餐、
          <span className="font-mono text-slate-400">/zen/</span> 默认），或选「自定义」填任意 OpenAI 兼容端点。Key 仅本地保存，除转发给上游外不会外发。
        </p>

        {upstreams.length === 0 ? (
          <div className="text-sm text-slate-500 py-4 text-center">
            还没有上游。点击「+ 添加上游」开始配置。
          </div>
        ) : (
          <div className="space-y-3">
            {upstreams.map((u, i) => (
              <div key={i} className="rounded-xl bg-white/[0.03] border border-white/[0.05] p-3">
                <div className="flex items-start gap-3">
                  <div className="flex-1 grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div>
                      <label className="label">Base URL</label>
                      <select
                        className="input font-mono mb-2"
                        value={ZEN_BASES.includes(u.base_url) ? u.base_url : "__custom__"}
                        onChange={(e) => {
                          if (e.target.value === "__custom__") {
                            // Switch to custom mode with a blank URL if it was a preset.
                            if (ZEN_BASES.includes(u.base_url)) {
                              updateUpstream(i, { base_url: "" });
                            }
                            return;
                          }
                          updateUpstream(i, { base_url: e.target.value });
                        }}
                      >
                        {ZEN_BASES.map((b) => (
                          <option key={b} value={b}>{b}</option>
                        ))}
                        <option value="__custom__">
                          {ZEN_BASES.includes(u.base_url) ? "自定义…" : "自定义（编辑下方）"}
                        </option>
                      </select>
                      {!ZEN_BASES.includes(u.base_url) && (
                        <input
                          className="input font-mono"
                          placeholder="https://your-custom-host/v1"
                          value={u.base_url}
                          onChange={(e) => updateUpstream(i, { base_url: e.target.value.trim() })}
                        />
                      )}
                    </div>
                    <div>
                      <label className="label">备注名</label>
                      <input
                        className="input"
                        placeholder="例如：go 套餐主号"
                        value={u.name}
                        onChange={(e) => updateUpstream(i, { name: e.target.value })}
                      />
                    </div>
                  </div>
                  <button
                    onClick={() => removeUpstream(i)}
                    className="btn-ghost !px-2.5 !py-2 text-slate-500 hover:text-accent-red shrink-0 mt-5"
                    title="删除"
                  >
                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </button>
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <div className="flex-1">
                    <label className="label">API Key {u.api_key_set ? `（当前：${u.api_key_masked}）` : ""}</label>
                    <input
                      type="password"
                      className="input font-mono"
                      placeholder={u.api_key_set ? "留空保持不变，或输入新 Key 替换" : "粘贴你的 Zen API Key"}
                      value={u.api_key}
                      onChange={(e) => updateUpstream(i, { api_key: e.target.value })}
                    />
                  </div>
                  <label className="flex items-center gap-2 cursor-pointer mt-5 shrink-0">
                    <button
                      type="button"
                      onClick={() => updateUpstream(i, { enabled: !u.enabled })}
                      className={`relative w-11 h-6 rounded-full transition-colors ${u.enabled ? "bg-accent" : "bg-ink-600"}`}
                    >
                      <span className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${u.enabled ? "translate-x-5" : ""}`} />
                    </button>
                    <span className="text-xs text-slate-400">启用</span>
                  </label>
                </div>

                <div className="mt-3 rounded-xl border border-white/[0.04] bg-ink-950/30 p-3">
                  <div className="flex items-center justify-between gap-3 mb-3">
                    <div>
                      <div className="text-xs font-medium text-slate-300">OpenCode Go 额度显示</div>
                      <p className="text-[11px] text-slate-500 mt-0.5">
                        填入 Workspace ID 与登录 Cookie 后，仪表盘会显示 5h、周限、月限。
                      </p>
                    </div>
                    {u.opencode_go_workspace_id && (u.opencode_go_auth_cookie_set || u.opencode_go_auth_cookie.trim()) ? (
                      <Badge tone="green">已配置</Badge>
                    ) : (
                      <Badge tone="amber">可选</Badge>
                    )}
                  </div>

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div>
                      <label className="label">Workspace ID</label>
                      <input
                        className="input font-mono"
                        placeholder={DEFAULT_OPENCODE_GO_WORKSPACE}
                        value={u.opencode_go_workspace_id}
                        onChange={(e) => updateUpstream(i, { opencode_go_workspace_id: e.target.value })}
                      />
                    </div>
                    <div>
                      <label className="label">
                        Auth Cookie {u.opencode_go_auth_cookie_set ? "（已保存）" : ""}
                      </label>
                      <input
                        type="password"
                        className="input font-mono"
                        placeholder={u.opencode_go_auth_cookie_set ? "留空保持不变，或输入新 Cookie 替换" : "auth=... 或 Cookie 值"}
                        value={u.opencode_go_auth_cookie}
                        onChange={(e) => updateUpstream(i, { opencode_go_auth_cookie: e.target.value })}
                      />
                    </div>
                  </div>

                  <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-slate-400">
                    <span className="text-slate-500">显示窗口：</span>
                    <label className="inline-flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={u.opencode_go_show_rolling}
                        onChange={(e) => updateUpstream(i, { opencode_go_show_rolling: e.target.checked })}
                      />
                      5h 限制
                    </label>
                    <label className="inline-flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={u.opencode_go_show_weekly}
                        onChange={(e) => updateUpstream(i, { opencode_go_show_weekly: e.target.checked })}
                      />
                      周限
                    </label>
                    <label className="inline-flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={u.opencode_go_show_monthly}
                        onChange={(e) => updateUpstream(i, { opencode_go_show_monthly: e.target.checked })}
                      />
                      月限
                    </label>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card className="mb-4">
        <div className="flex items-center gap-2 mb-4">
          <SlidersIcon />
          <h3 className="text-sm font-semibold text-slate-200">行为</h3>
        </div>

        <Toggle
          label="Anthropic 智能原生路由"
          desc="开启后，仅 claude-* / qwen* 目标模型直连上游 /v1/messages；glm、deepseek、kimi 等其它目标模型继续走转换模式。"
          checked={nativeAnthropic}
          onChange={(v) => {
            setNativeAnthropic(v);
            setDirty(true);
          }}
        />

        <div className="mt-4">
          <Toggle
            label="记录请求"
            desc="为面板记录每个代理请求及其转换后的响应。"
            checked={logReqs}
            onChange={(v) => {
              setLogReqs(v);
              setDirty(true);
            }}
          />
        </div>

        <div className="mt-4">
          <Toggle
            label="要求 API 密钥"
            desc="开启后，/v1/* 代理端点必须携带有效的客户端密钥（见「API 密钥」页）。请先创建密钥再开启。"
            checked={requireKey}
            onChange={(v) => {
              setRequireKey(v);
              setDirty(true);
            }}
          />
        </div>

        {nativeAnthropic && (
          <p className="mt-3 rounded-xl border border-amber-400/20 bg-amber-400/10 px-3 py-2 text-xs text-amber-100">
            请确认当前上游 Base URL 对 Claude/Qwen 模型支持 <span className="font-mono">/v1/messages</span>。
            非 Anthropic 原生目标模型不会走这条直连路径。
          </p>
        )}

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

      <Card className="mb-4">
        <div className="flex items-center gap-2 mb-4">
          <CacheIcon />
          <h3 className="text-sm font-semibold text-slate-200">Prompt Cache 优化</h3>
          {promptCache ? <Badge tone="green">已开启</Badge> : <Badge tone="amber">已关闭</Badge>}
        </div>

        <Toggle
          label="启用缓存友好化"
          desc="自动添加 prompt_cache_key，并稳定工具、system/developer 前缀和相邻文件上下文顺序。"
          checked={promptCache}
          onChange={(v) => {
            setPromptCache(v);
            setDirty(true);
          }}
        />

        <div className="mt-4">
          <label className="label">prompt_cache_key 前缀</label>
          <input
            className="input font-mono"
            value={promptCacheKeyPrefix}
            onChange={(e) => {
              setPromptCacheKeyPrefix(e.target.value);
              setDirty(true);
            }}
          />
          <p className="text-xs text-slate-500 mt-2">
            代理会基于模型、工具集和稳定 system 前缀生成 key；这里的前缀用于区分不同代理实例。
          </p>
        </div>

        <div className="mt-4">
          <Toggle
            label="Anthropic 自动 cache_control"
            desc="原生 Anthropic 上游请求中，如果有稳定 system/tool 前缀但没有 cache_control，则自动添加 ephemeral 缓存断点。"
            checked={promptCacheAnthropicControl}
            onChange={(v) => {
              setPromptCacheAnthropicControl(v);
              setDirty(true);
            }}
          />
        </div>

        <div className="mt-4">
          <Toggle
            label="归一化缓存前缀"
            desc="移除 request_id/timestamp 等非 prompt 噪声字段，并固定工具、system/developer、文件上下文顺序。"
            checked={promptCacheNormalize}
            onChange={(v) => {
              setPromptCacheNormalize(v);
              setDirty(true);
            }}
          />
        </div>
      </Card>

      <Card className="mb-4">
        <div className="flex items-center gap-2 mb-4">
          <LockIcon />
          <h3 className="text-sm font-semibold text-slate-200">面板访问密码</h3>
          {cfg.panel_token_set ? <Badge tone="green">已设置</Badge> : <Badge tone="amber">未设置（开放访问）</Badge>}
        </div>
        <p className="text-xs text-slate-500 mb-4">
          设置密码后，访问控制面板需先通过登录页验证。留空并保存可清除密码、恢复开放访问。
        </p>

        <label className="label">新密码</label>
        <input
          type="password"
          className="input mb-3"
          placeholder="输入新密码，留空则清除"
          value={newPanelToken}
          autoComplete="new-password"
          onChange={(e) => setNewPanelToken(e.target.value)}
        />

        <label className="label">确认新密码</label>
        <input
          type="password"
          className="input mb-3"
          placeholder="再次输入新密码"
          value={confirmPanelToken}
          autoComplete="new-password"
          onChange={(e) => setConfirmPanelToken(e.target.value)}
        />

        {panelTokenError && (
          <p className="text-xs text-accent-red mb-3">{panelTokenError}</p>
        )}

        <div className="flex items-center gap-3">
          <button
            onClick={savePanelToken}
            disabled={saving}
            className="btn-primary"
          >
            {saving ? <Spinner /> : <SaveIcon />}
            {newPanelToken === "" ? "清除密码" : "设置密码"}
          </button>
          {panelTokenFlash && (
            <span className="text-xs text-accent-green">{panelTokenFlash}</span>
          )}
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
export ANTHROPIC_AUTH_TOKEN=${requireKey ? "sk-your-client-key" : "anything"}
claude

# PowerShell
$env:ANTHROPIC_BASE_URL="http://localhost:${cfg.listen_addr.split(":").pop() || "8787"}"
$env:ANTHROPIC_AUTH_TOKEN="${requireKey ? "sk-your-client-key" : "anything"}"
claude`}
        </pre>
        <p className="text-xs text-slate-500 mt-3">
          {requireKey
            ? "已启用客户端鉴权，请使用「API 密钥」页创建的密钥作为 AUTH_TOKEN。"
            : "未启用客户端鉴权时，AUTH_TOKEN 的值无关紧要；代理仍使用你的 Zen Key 向上游鉴权。"}
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
function LockIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-slate-500">
      <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
      <path d="M7 11V7a5 5 0 0 1 10 0v4" strokeLinecap="round" />
    </svg>
  );
}

function CacheIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-slate-500">
      <ellipse cx="12" cy="5" rx="8" ry="3" />
      <path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5" />
      <path d="M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6" />
    </svg>
  );
}
