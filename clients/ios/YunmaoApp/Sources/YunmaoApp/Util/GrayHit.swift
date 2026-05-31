// 与 pkg/yunmao/featureflags FNV1a Hash100 等价的 client-side 灰度命中。

import Foundation

public enum GrayHit {
    /// 与后端 Hash100 完全等价：FNV-1a 32bit hash % 100
    public static func hash100(_ key: String) -> Int {
        var hash: UInt32 = 0x811C9DC5
        for b in key.utf8 {
            hash ^= UInt32(b)
            hash = hash &* 0x01000193
        }
        return Int(hash % 100)
    }

    /// percent ∈ [0, 100]；返回 true 代表本 room 命中灰度。
    public static func inGrayPercent(roomID: String, percent: Int) -> Bool {
        if percent <= 0 { return false }
        if percent >= 100 { return true }
        return hash100(roomID) < percent
    }
}
