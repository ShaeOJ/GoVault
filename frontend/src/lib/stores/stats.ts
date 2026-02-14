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
  stratumRunning: boolean;
  blockHeight: number;
}

export interface HashratePoint {
  t: number;
  h: number;
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
  stratumRunning: false,
  blockHeight: 0,
});

export const hashrateHistory = writable<HashratePoint[]>([]);
export const blockFound = writable<{ hash: string; height: number } | null>(null);
