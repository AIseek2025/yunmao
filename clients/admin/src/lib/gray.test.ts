import { describe, expect, it } from "vitest";
import { hash100, inGrayPercent } from "./gray";

describe("admin gray", () => {
  it("deterministic", () => {
    expect(hash100("room_demo")).toBe(hash100("room_demo"));
  });
  it("matches web client distribution (~50%)", () => {
    let h = 0;
    for (let i = 0; i < 4000; i++) {
      if (inGrayPercent(`r_${i}`, 50)) h++;
    }
    const pct = (h / 4000) * 100;
    expect(pct).toBeGreaterThan(46);
    expect(pct).toBeLessThan(54);
  });
});
