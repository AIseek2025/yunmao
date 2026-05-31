package live.yunmao.app.util

// 与后端 pkg/yunmao/featureflags Hash100 / iOS GrayHit 完全等价的 FNV-1a 32bit。
object GrayHit {
    fun hash100(key: String): Int {
        var hash: Int = 0x811C9DC5.toInt()
        for (b in key.toByteArray(Charsets.UTF_8)) {
            hash = hash xor (b.toInt() and 0xff)
            hash = (hash * 0x01000193).toInt()
        }
        // 取无符号 mod 100
        val unsigned = hash.toLong() and 0xFFFFFFFFL
        return (unsigned % 100).toInt()
    }

    fun inGrayPercent(roomId: String, percent: Int): Boolean {
        if (percent <= 0) return false
        if (percent >= 100) return true
        return hash100(roomId) < percent
    }
}
