import { writable } from 'svelte/store';

export interface LogEntry {
  timestamp: string;
  level: string;
  component: string;
  message: string;
}

export const logs = writable<LogEntry[]>([]);
