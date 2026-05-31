package live.yunmao.app

import live.yunmao.app.util.GrayHit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GrayHitTest {
    @Test fun boundaries() {
        assertFalse(GrayHit.inGrayPercent("any", 0))
        assertTrue(GrayHit.inGrayPercent("any", 100))
    }

    @Test fun distribution_20pct() {
        val target = 20
        val samples = 5000
        var hits = 0
        for (i in 0 until samples) {
            if (GrayHit.inGrayPercent("room_test_$i", target)) hits++
        }
        val pct = hits * 100.0 / samples
        assertTrue("expected 20% ± 4%, got $pct", pct in 16.0..24.0)
    }

    @Test fun deterministic() {
        assertEquals(GrayHit.hash100("room_x"), GrayHit.hash100("room_x"))
    }

    @Test fun crossClientFNV1aConsistency() {
        val knownValues = mapOf(
            "" to 61,
            "a" to 20,
            "ab" to 46,
            "abc" to 31,
            "room_cross_client_e2e" to 84,
        )
        for ((key, expected) in knownValues) {
            val actual = GrayHit.hash100(key)
            assertEquals("FNV1a hash100('$key') should be $expected", expected, actual)
        }
    }

    @Test fun inGrayPercent_boundaryValues() {
        assertFalse(GrayHit.inGrayPercent("room1", -1))
        assertFalse(GrayHit.inGrayPercent("room1", 0))
        assertTrue(GrayHit.inGrayPercent("room1", 100))
        assertTrue(GrayHit.inGrayPercent("room1", 101))
    }

    @Test fun distribution_50pct() {
        val samples = 5000
        var hits = 0
        for (i in 0 until samples) {
            if (GrayHit.inGrayPercent("room_50_$i", 50)) hits++
        }
        val pct = hits * 100.0 / samples
        assertTrue("expected 50% ± 4%, got $pct", pct in 46.0..54.0)
    }
}
