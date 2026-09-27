export interface Stats {
  keys: number;
  hits: number;
  misses: number;
  ops: number;
}

export interface KeyRow {
  key: string;
  value: string;
  ttl: number;
}
