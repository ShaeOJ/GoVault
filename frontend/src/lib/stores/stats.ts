import { writable } from 'svelte/store';

export interface DashboardStats {
  totalHashrate: number;
  activeMiners: number;
  sharesAccepted: number;
  sharesRejected: number;
  poolShares: number;
  bestDifficulty: number;
  blocksFound: number;
  networkDifficulty: number;
  networkHashrate: number;
  estTimeToBlock: number;
  blockChance: number;
  stratumRunning: boolean;
  blockHeight: number;
  stratumUptimeSec: number;
  sharesPerSecond: number;
  coinId: string;
  coinSymbol: string;
  // Proxy mode fields
  miningMode: string;
  upstreamDiff: number;
  proxySharesFwd: number;
  proxySharesAccepted: number;
  proxySharesRejected: number;
  poolPingMs: number;
}

export interface HashratePoint {
  t: number;
  h: number;
  s: number;
}

export const dashboardStats = writable<DashboardStats>({
  totalHashrate: 0,
  activeMiners: 0,
  sharesAccepted: 0,
  sharesRejected: 0,
  poolShares: 0,
  bestDifficulty: 0,
  blocksFound: 0,
  networkDifficulty: 0,
  networkHashrate: 0,
  estTimeToBlock: 0,
  blockChance: 0,
  stratumRunning: false,
  blockHeight: 0,
  stratumUptimeSec: 0,
  sharesPerSecond: 0,
  coinId: '',
  coinSymbol: '',
  miningMode: 'solo',
  upstreamDiff: 0,
  proxySharesFwd: 0,
  proxySharesAccepted: 0,
  proxySharesRejected: 0,
  poolPingMs: 0,
});

export const hashrateHistory = writable<HashratePoint[]>([]);
export const blockFound = writable<{ hash: string; height: number } | null>(null);
