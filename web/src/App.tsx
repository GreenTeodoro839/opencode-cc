import { ReactNode, useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";

function Logo() {
  return (
    <div className="flex items-center gap-2.5">
      <div className="w-9 h-9 rounded-xl bg-gradient-to-br from-accent to-accent-cyan flex items-center justify-center shadow-glow">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
          <path d="M6 8l6 8 6-8" />
        </svg>
      </div>
      <div className="leading-tight">
        <div className="text-sm font-semibold text-white tracking-tight">opencode-cc</div>
        <div className="text-[10px] text-slate-500 font-mono">anthropic ⇄ openai</div>
      </div>
    </div>
  );
}

const NAV = [
  { to: "/", label: "仪表盘", icon: DashIcon, end: true },
  { to: "/logs", label: "请求日志", icon: ListIcon },
  { to: "/models", label: "模型路由", icon: CubeIcon },
  { to: "/settings", label: "设置", icon: GearIcon },
];

function NavItem({
  to,
  label,
  icon: Icon,
  end,
}: {
  to: string;
  label: string;
  icon: (p: { className?: string }) => JSX.Element;
  end?: boolean;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        `group flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-all ${
          isActive
            ? "bg-white/[0.06] text-white border border-white/[0.06] shadow-card"
            : "text-slate-400 hover:text-slate-200 hover:bg-white/[0.03] border border-transparent"
        }`
      }
    >
      <Icon className="w-[18px] h-[18px]" />
      {label}
    </NavLink>
  );
}

function StatusBar() {
  const [online, setOnline] = useState<boolean | null>(null);
  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const r = await fetch("/api/health");
        setOnline(r.ok);
      } catch {
        setOnline(false);
      }
      return () => {
        alive = false;
      };
    };
    tick();
    const id = setInterval(tick, 15000);
    return () => {
      clearInterval(id);
      alive = false;
    };
  }, []);

  const tone =
    online === null
      ? "bg-slate-500"
      : online
      ? "bg-accent-green"
      : "bg-accent-red";
  const text =
    online === null ? "连接中…" : online ? "代理在线" : "代理离线";

  return (
    <div className="flex items-center gap-2 text-xs text-slate-400">
      <span className={`relative flex h-2 w-2`}>
        <span
          className={`absolute inline-flex h-full w-full rounded-full ${tone} opacity-60 animate-pulse-dot`}
        />
        <span className={`relative inline-flex rounded-full h-2 w-2 ${tone}`} />
      </span>
      {text}
    </div>
  );
}

export default function App() {
  return (
    <div className="min-h-screen flex">
      {/* Sidebar */}
      <aside className="hidden md:flex w-64 shrink-0 flex-col border-r border-white/[0.05] bg-ink-950/40 backdrop-blur-xl">
        <div className="px-5 py-5">
          <Logo />
        </div>
        <nav className="px-3 flex flex-col gap-1">
          {NAV.map((n) => (
            <NavItem key={n.to} {...n} />
          ))}
        </nav>
        <div className="mt-auto px-5 py-4 border-t border-white/[0.05]">
          <StatusBar />
        </div>
      </aside>

      {/* Mobile top bar */}
      <div className="md:hidden fixed top-0 inset-x-0 z-20 bg-ink-950/80 backdrop-blur-xl border-b border-white/[0.05]">
        <div className="flex items-center justify-between px-4 py-3">
          <Logo />
          <StatusBar />
        </div>
        <nav className="flex px-2 pb-2 gap-1 overflow-x-auto">
          {NAV.map((n) => (
            <NavItem key={n.to} {...n} />
          ))}
        </nav>
      </div>

      {/* Content */}
      <main className="flex-1 min-w-0 px-5 md:px-8 py-6 md:py-8 pt-24 md:pt-8 max-w-[1400px] mx-auto w-full">
        <Outlet />
      </main>
    </div>
  );
}

// --- Icons (inline SVGs, no extra deps) ---
type IconProps = { className?: string };
function DashIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
    </svg>
  );
}
function ListIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M8 6h13M8 12h13M8 18h13" strokeLinecap="round" />
      <circle cx="3.5" cy="6" r="1.3" fill="currentColor" />
      <circle cx="3.5" cy="12" r="1.3" fill="currentColor" />
      <circle cx="3.5" cy="18" r="1.3" fill="currentColor" />
    </svg>
  );
}
function CubeIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M12 2l9 5v10l-9 5-9-5V7l9-5z" strokeLinejoin="round" />
      <path d="M3 7l9 5 9-5M12 12v10" />
    </svg>
  );
}
function GearIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-2.82 1.17V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 8 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 3 15a1.65 1.65 0 0 0-1.51-1H1a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 3 8.6a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 8 4.6h.09A1.65 1.65 0 0 0 9 3.09V3a2 2 0 1 1 4 0v.09c0 .67.39 1.27 1 1.51.61.24 1.31.11 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.24-.61.84-1 1.51-1H21a2 2 0 1 1 0 4h-.09c-.67 0-1.27.39-1.51 1z" />
    </svg>
  );
}

export function PageHeader({ title, desc, actions }: { title: string; desc?: string; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4 mb-6">
      <div>
        <h1 className="text-xl font-semibold text-white tracking-tight">{title}</h1>
        {desc && <p className="text-sm text-slate-500 mt-1">{desc}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
