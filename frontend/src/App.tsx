import { useEffect, useState } from "react";
import type { KeyRow, Stats } from "./types";

function StatCard({ label, value, accent }: { label: string; value: React.ReactNode; accent?: string }) {
  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-4">
      <div className="text-xs text-slate-400">{label}</div>
      <div className={`text-2xl font-semibold font-mono ${accent ?? ""}`}>{value}</div>
    </div>
  );
}

function ttlLabel(t: number) {
  if (t === -1) return <span className="text-slate-500">∞</span>;
  if (t === -2) return <span className="text-rose-400">expired</span>;
  return <span className="text-cyan-300 font-mono">{t}s</span>;
}

export default function App() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [keys, setKeys] = useState<KeyRow[]>([]);

  useEffect(() => {
    const refresh = async () => {
      try {
        const [s, k] = await Promise.all([
          fetch("/api/stats").then((r) => r.json() as Promise<Stats>),
          fetch("/api/keys").then((r) => r.json() as Promise<KeyRow[]>),
        ]);
        setStats(s);
        setKeys(k);
      } catch {
        /* server restarting */
      }
    };
    refresh();
    const id = setInterval(refresh, 1000);
    return () => clearInterval(id);
  }, []);

  const total = stats ? stats.hits + stats.misses : 0;
  const hitRate = total > 0 ? Math.round((100 * (stats?.hits ?? 0)) / total) : 0;

  return (
    <div className="max-w-6xl mx-auto px-6 py-8">
      <header className="flex items-center gap-3 mb-8">
        <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-fuchsia-400 to-indigo-500 grid place-items-center font-bold text-slate-900">
          CN
        </div>
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">CacheNova</h1>
          <p className="text-sm text-slate-400">RESP2-compatible in-memory cache · sessions · rate limits</p>
        </div>
      </header>

      <section className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
        <StatCard label="Keys" value={stats?.keys ?? 0} accent="text-fuchsia-300" />
        <StatCard label="Total Ops" value={stats?.ops ?? 0} />
        <StatCard label="Hits / Misses" value={`${stats?.hits ?? 0} / ${stats?.misses ?? 0}`} />
        <StatCard label="Hit Rate" value={`${hitRate}%`} accent="text-emerald-300" />
      </section>

      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-sm uppercase tracking-widest text-slate-500">Keys</h2>
          <span className="text-xs text-slate-500">
            Try: <code className="text-fuchsia-300">redis-cli -p 6380 set user:1 alice</code>
          </span>
        </div>
        <div className="overflow-hidden rounded-2xl border border-slate-800">
          <table className="w-full text-sm">
            <thead className="bg-slate-900/80 text-slate-400">
              <tr>
                <th className="text-left px-4 py-3 font-medium">Key</th>
                <th className="text-left px-4 py-3 font-medium">Value</th>
                <th className="text-left px-4 py-3 font-medium">TTL</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {keys.length === 0 ? (
                <tr>
                  <td colSpan={3} className="px-4 py-8 text-center text-slate-500">
                    No keys yet — set one with redis-cli.
                  </td>
                </tr>
              ) : (
                keys.map((row) => (
                  <tr key={row.key} className="hover:bg-slate-900/40">
                    <td className="px-4 py-3 font-mono text-fuchsia-200">{row.key}</td>
                    <td className="px-4 py-3 font-mono text-slate-300 truncate max-w-md">{row.value}</td>
                    <td className="px-4 py-3">{ttlLabel(row.ttl)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
