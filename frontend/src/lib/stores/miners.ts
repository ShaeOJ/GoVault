import { writable } from 'svelte/store';

export interface MinerInfo {
  id: string;
  workerName: string;
  userAgent: string;
  ipAddress: string;
  connectedAt: string;
  currentDiff: number;
  hashrate: number;
  sharesAccepted: number;
  sharesRejected: number;
  bestDifficulty: number;
  lastShareTime: string;
}

export interface DiscoveredMiner {
  ip: string;
  hostname: string;
  model: string;
  hashrate: number;
  temperature: number;
  currentPool: string;
  firmware: string;
}

export const miners = writable<MinerInfo[]>([]);
export const selectedMiner = writable<MinerInfo | null>(null);
export const discoveredMiners = writable<DiscoveredMiner[]>([]);
