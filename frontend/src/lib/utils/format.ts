export function formatHashrate(hashesPerSec: number): string {
  if (hashesPerSec <= 0) return '0 H/s';
  const units = ['H/s', 'KH/s', 'MH/s', 'GH/s', 'TH/s', 'PH/s', 'EH/s'];
  let idx = 0;
  let val = hashesPerSec;
  while (val >= 1000 && idx < units.length - 1) {
    val /= 1000;
    idx++;
  }
  return `${val.toFixed(2)} ${units[idx]}`;
}

export function formatDifficulty(diff: number): string {
  if (diff <= 0) return '0';
  if (diff >= 1e15) return `${(diff / 1e15).toFixed(2)}P`;
  if (diff >= 1e12) return `${(diff / 1e12).toFixed(2)}T`;
  if (diff >= 1e9) return `${(diff / 1e9).toFixed(2)}G`;
  if (diff >= 1e6) return `${(diff / 1e6).toFixed(2)}M`;
  if (diff >= 1e3) return `${(diff / 1e3).toFixed(2)}K`;
  if (diff >= 1) return diff.toFixed(2);
  return diff.toFixed(6);
}

export function formatDuration(seconds: number): string {
  if (seconds <= 0) return 'N/A';
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  if (seconds < 86400 * 365) {
    const days = Math.floor(seconds / 86400);
    return `${days}d ${Math.floor((seconds % 86400) / 3600)}h`;
  }
  const years = Math.floor(seconds / (86400 * 365));
  if (years >= 1000) return `${(years / 1000).toFixed(1)}K years`;
  return `~${years.toLocaleString()} years`;
}

export function formatNumber(n: number): string {
  if (n >= 1e12) return `${(n / 1e12).toFixed(2)}T`;
  if (n >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  return n.toLocaleString();
}

export function timeAgo(dateStr: string): string {
  if (!dateStr) return 'never';
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const diff = Math.floor((now - then) / 1000);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}
