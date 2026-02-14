import { writable } from 'svelte/store';

export interface NodeStatus {
  connected: boolean;
  syncing?: boolean;
  blockHeight: number;
  syncPercent?: number;
  networkDifficulty: number;
  networkHashrate: number;
  chain?: string;
  nodeVersion?: string;
  connections?: number;
}

export const nodeStatus = writable<NodeStatus>({
  connected: false,
  blockHeight: 0,
  networkDifficulty: 0,
  networkHashrate: 0,
});
