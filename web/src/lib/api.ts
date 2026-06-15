// Tiny fetch wrapper for the panel API. All endpoints live under /api.

export interface Summary {
  total_requests: number;
  total_errors: number;
  total_input_tokens: number;
  total_output_tokens: number;
  requests_last_24h: number;
  errors_last_24h: number;
}

export interface HourPoint {
  hour: number; // epoch seconds
  requests: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
}

export interface ModelUsagePoint {
  model: string;
  requests: number;
  tokens: number;
}

export interface Latency {
  p50: number;
  p95: number;
  p99: number;
}

export interface LogRow {
  id: number;
  ts: string;
  method: string;
  path: string;
  incoming_model: string;
  target_model: string;
  stream: boolean;
  status: number;
  duration_ms: number;
  input_tokens: number;
  output_tokens: number;
  stop_reason: string;
  error: string;
  req_body?: string;
  resp_body?: string;
}

export interface PanelConfig {
  listen_addr: string;
  upstream_base: string;
  zen_api_key_masked: string;
  zen_api_key_set: boolean;
  panel_token_set: boolean;
  default_model: string;
  model_mappings: { match: string; target: string }[];
  log_requests: boolean;
  max_body_log_bytes: number;
  request_timeout_seconds: number;
}

export interface TestResult {
  ok: boolean;
  model: string;
  elapsed_ms: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  preview?: string;
  error?: string;
}

async function asJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const t = await res.text().catch(() => res.statusText);
    throw new Error(t || `HTTP ${res.status}`);
  }
  return (await res.json()) as T;
}

export const api = {
  health: () => fetch("/api/health").then((r) => asJson<{ ok: boolean }>(r)),
  summary: () => fetch("/api/stats/summary").then((r) => asJson<Summary>(r)),
  hourly: (hours = 24) =>
    fetch(`/api/stats/hourly?hours=${hours}`).then((r) => asJson<HourPoint[]>(r)),
  models: (hours = 24) =>
    fetch(`/api/stats/models?hours=${hours}`).then((r) => asJson<ModelUsagePoint[]>(r)),
  latency: () => fetch("/api/stats/latency").then((r) => asJson<Latency>(r)),
  logs: (limit = 100) =>
    fetch(`/api/logs?limit=${limit}`).then((r) => asJson<LogRow[]>(r)),
  log: (id: number) => fetch(`/api/logs/${id}`).then((r) => asJson<LogRow>(r)),
  getConfig: () => fetch("/api/config").then((r) => asJson<PanelConfig>(r)),
  putConfig: (body: unknown) =>
    fetch("/api/config", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then((r) => asJson<PanelConfig>(r)),
  test: (model?: string) =>
    fetch(`/api/test${model ? `?model=${encodeURIComponent(model)}` : ""}`).then((r) =>
      asJson<TestResult>(r)
    ),
};
