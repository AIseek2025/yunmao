// 与 Go pkg/yunmao/featureflags / iOS GrayHit / Android GrayHit 同算法的 FNV1a 32bit。

export function hash100(key: string): number {
  let hash = 0x811c9dc5 | 0;
  const enc = new TextEncoder().encode(key);
  for (let i = 0; i < enc.length; i++) {
    hash = (hash ^ enc[i]) >>> 0;
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash % 100;
}

export function inGrayPercent(key: string, percent: number): boolean {
  if (percent <= 0) return false;
  if (percent >= 100) return true;
  return hash100(key) < percent;
}
