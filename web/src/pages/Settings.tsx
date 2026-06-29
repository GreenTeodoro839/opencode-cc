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
  protocol: string;
  models: UpstreamModelRow[];
  api_key_set: boolean;
  api_key_masked: string;
}

interface UpstreamModelRow {
  name: string;
  alias: string;
  match?: string;
  target?: string;
}

const ZEN_BASES = [
  "https://opencode.ai/zen/go",
  "https://opencode.ai/zen/",
];

export default function Settings() {
  const [cfg, setCfg] = useState<PanelConfig | null>(null);
  const [upstreams, setUpstreams] = useState<UpstreamRow[]>([]);
  const [nativeAnthropic, setNativeAnthropic] = useState(false);
  const [logReqs, setLogReqs] = useState(true);
  const [maxBody, setMaxBody] = useState(16384);
  const [timeout, setTimeoutSecs] = useState(0);
  const [requireKey, setRequireKey] = useState(false);
  const [webSearchModel, setWebSearchModel] = useState("");
  const [webSearchMode, setWebSearchMode] = useState("auto");
  const [webSearchBaseURL, setWebSearchBaseURL] = useState("");
  const [webSearchAPIKey, setWebSearchAPIKey] = useState("");
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
      setWebSearchModel(c.web_search_model || "");
      setWebSearchMode(c.web_search_mode || "auto");
      setWebSearchBaseURL(c.web_search_base_url || "");
      setWebSearchAPIKey("");
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
        protocol: u.protocol || "auto",
        models: (u.models || []).map((m) => ({
          name: m.name || m.target || "",
          alias: m.alias || (m.match && m.match !== "*" ? m.match : ""),
          match: m.match,
          target: m.target,
        })),
        api_key_set: u.api_key_set,
        api_key_masked: u.api_key_masked,
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
        web_search_model: webSearchModel.trim(),
        web_search_mode: webSearchMode,
        web_search_base_url: webSearchBaseURL.trim(),
        web_search_api_key: webSearchAPIKey.trim(),
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
          protocol: u.protocol || "auto",
          models: u.models
            .map((m) => ({
              name: m.name.trim(),
              alias: m.alias.trim(),
              match: (m.match || "").trim(),
              target: (m.target || "").trim(),
            }))
            .filter((m) => m.name || m.alias || m.match || m.target),
        })),
      };
      const updated = await api.putConfig(body);
      setCfg(updated);
      setWebSearchBaseURL(updated.web_search_base_url || "");
      setWebSearchAPIKey("");
      // Refresh masked key views from the server response.
      if (updated.upstreams) {
        setUpstreams(
          updated.upstreams.map((u) => ({
            base_url: u.base_url,
            api_key: "",
            name: u.name,
            enabled: u.enabled,
            protocol: u.protocol || "auto",
            models: (u.models || []).map((m) => ({
              name: m.name || m.target || "",
              alias: m.alias || (m.match && m.match !== "*" ? m.match : ""),
              match: m.match,
              target: m.target,
            })),
            api_key_set: u.api_key_set,
            api_key_masked: u.api_key_masked,
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
        protocol: "auto",
        models: [],
        api_key_set: false,
        api_key_masked: "",
      },
    ]);
    setDirty(true);
  }
  function removeUpstream(i: number) {
    setUpstreams((prev) => prev.filter((_, idx) => idx !== i));
    setDirty(true);
  }
  function updateModel(upstreamIndex: number, modelIndex: number, patch: Partial<UpstreamModelRow>) {
    setUpstreams((prev) =>
      prev.map((u, idx) =>
        idx === upstreamIndex
          ? {
              ...u,
              models: u.models.map((m, j) => (j === modelIndex ? { ...m, ...patch } : m)),
            }
          : u
      )
    );
    setDirty(true);
  }
  function addModel(upstreamIndex: number) {
    setUpstreams((prev) =>
      prev.map((u, idx) =>
        idx === upstreamIndex
          ? { ...u, models: [...u.models, { alias: "", name: "" }] }
          : u
      )
    );
    setDirty(true);
  }
  function removeModel(upstreamIndex: number, modelIndex: number) {
    setUpstreams((prev) =>
      prev.map((u, idx) =>
        idx === upstreamIndex
          ? { ...u, models: u.models.filter((_, j) => j !== modelIndex) }
          : u
      )
    );
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
            <h3 className="text-sm font-semibold text-slate-200">上游与模型路由</h3>
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
          为每个上游指定协议和接入模型。只要任一上游配置了模型表，请求会按 alias 精确路由，不再按请求轮询。Base URL 可从下拉选预设（
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
                <div className="mt-3 grid grid-cols-1 sm:grid-cols-[180px_1fr] gap-3">
                  <div>
                    <label className="label">上游协议</label>
                    <select
                      className="input"
                      value={u.protocol || "auto"}
                      onChange={(e) => updateUpstream(i, { protocol: e.target.value })}
                    >
                      <option value="auto">Auto</option>
                      <option value="openai">OpenAI Chat</option>
                      <option value="anthropic">Anthropic Messages</option>
                    </select>
                  </div>
                  <div>
                    <div className="flex items-center justify-between">
                      <label className="label">模型路由</label>
                      <button onClick={() => addModel(i)} className="btn-ghost !py-1 !text-xs">
                        + 模型
                      </button>
                    </div>
                    {u.models.length === 0 ? (
                      <div className="rounded-lg border border-white/[0.05] bg-ink-900/40 px-3 py-2 text-xs text-slate-500">
                        未配置模型表时沿用旧映射与轮询；配置后只接入表内 alias。
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {u.models.map((m, j) => (
                          <div key={j} className="grid grid-cols-1 sm:grid-cols-[1fr_1fr_auto] gap-2">
                            <input
                              className="input font-mono"
                              placeholder="客户端 alias，例如 claude-sonnet"
                              value={m.alias}
                              onChange={(e) => updateModel(i, j, { alias: e.target.value })}
                            />
                            <input
                              className="input font-mono"
                              placeholder="上游 model，例如 deepseek-chat"
                              value={m.name}
                              onChange={(e) => updateModel(i, j, { name: e.target.value })}
                            />
                            <button
                              onClick={() => removeModel(i, j)}
                              className="btn-ghost !px-2.5 text-slate-500 hover:text-accent-red"
                              title="删除模型"
                            >
                              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                                <path d="M18 6 6 18M6 6l12 12" strokeLinecap="round" />
                              </svg>
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
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

        <div className="grid grid-cols-2 gap-4 mt-4">
          <div>
            <label className="label">Web Search 模式</label>
            <select
              className="input"
              value={webSearchMode}
              onChange={(e) => {
                setWebSearchMode(e.target.value);
                setDirty(true);
              }}
            >
              <option value="auto">Auto</option>
              <option value="native">Native</option>
              <option value="translate">Translate</option>
            </select>
            <p className="text-xs text-slate-500 mt-2">
              Native 可使用独立 Anthropic 上游；Translate 使用代理搜索并整理结果。
            </p>
          </div>
          <div>
            <label className="label">Web Search 模型（留空 = 沿用主模型）</label>
            <input
              className="input font-mono"
              placeholder="例如：deepseek-v4-flash / glm-5-air"
              value={webSearchModel}
              onChange={(e) => {
                setWebSearchModel(e.target.value);
                setDirty(true);
              }}
            />
            <p className="text-xs text-slate-500 mt-2">
              Native 和 Translate 都会使用；留空时沿用主模型。
            </p>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4 mt-4">
          <div>
            <label className="label">Web Search 原生上游</label>
            <input
              className="input font-mono"
              placeholder="https://api.deepseek.com/anthropic"
              value={webSearchBaseURL}
              onChange={(e) => {
                setWebSearchBaseURL(e.target.value);
                setDirty(true);
              }}
            />
            <p className="text-xs text-slate-500 mt-2">
              仅 Native 模式使用；留空时复用主上游。
            </p>
          </div>
          <div>
            <label className="label">
              Web Search API Key
              {cfg?.web_search_api_key_set ? (
                <span className="ml-2 text-xs text-slate-500">已设置 {cfg.web_search_api_key_masked}</span>
              ) : null}
            </label>
            <input
              className="input font-mono"
              placeholder={cfg?.web_search_api_key_set ? "留空 = 保持当前 key" : "DeepSeek API key"}
              type="password"
              value={webSearchAPIKey}
              onChange={(e) => {
                setWebSearchAPIKey(e.target.value);
                setDirty(true);
              }}
            />
            <p className="text-xs text-slate-500 mt-2">
              留空不会覆盖已保存的搜索专用 key。
            </p>
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
