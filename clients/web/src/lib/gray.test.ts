import { describe, expect, it } from "vitest";
import { hash100, inGrayPercent } from "./gray";

describe("gray", () => {
  it("boundary", () => {
    expect(inGrayPercent("anything", 0)).toBe(false);
    expect(inGrayPercent("anything", 100)).toBe(true);
  });
  it("deterministic", () => {
    expect(hash100("room_demo")).toBe(hash100("room_demo"));
  });
  it("approximately 20% over 5000 keys", () => {
    let hits = 0;
    for (let i = 0; i < 5000; i++) {
      if (inGrayPercent(`room_${i}`, 20)) hits++;
    }
    const pct = (hits / 5000) * 100;
    expect(pct).toBeGreaterThan(16);
    expect(pct).toBeLessThan(24);
  });
});
