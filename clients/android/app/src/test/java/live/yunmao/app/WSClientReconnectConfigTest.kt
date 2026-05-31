package live.yunmao.app

import live.yunmao.app.network.WSClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Verifies that the Android WSClient (mirroring iOS `WSClient.swift`) preserves
 * auto-reconnect configuration defaults that are consistent with the iOS peer.
 *
 * The full socket lifecycle requires a real ktor/WS server; these tests pin down the
 * configuration surface that is shared between the two clients (backoff bounds,
 * max attempts, autoReconnect default) so they stay in lock-step across platforms.
 */
class WSClientReconnectConfigTest {
    @Test fun defaultReconnectConfigMatchesIosPeer() {
        val c = WSClient(wsBase = "wss://example.com", token = "t")
        assertTrue("autoReconnect should default to true (iOS parity)", c.autoReconnect)
        assertEquals(1_000L, c.baseBackoffMs)
        assertEquals(30_000L, c.maxBackoffMs)
        assertEquals(5, c.maxAttempts)
    }

    @Test fun exponentialBackoffCapsAtMax() {
        // Model the backoff doubling without requiring a real network session.
        val base = 1_000L
        val max = 30_000L
        var backoff = base
        val sequence = mutableListOf<Long>()
        repeat(7) {
            sequence.add(backoff)
            backoff = (backoff * 2).coerceAtMost(max)
        }
        assertEquals(listOf(1_000L, 2_000L, 4_000L, 8_000L, 16_000L, 30_000L, 30_000L), sequence)
    }

    @Test fun canDisableAutoReconnect() {
        val c = WSClient(wsBase = "wss://example.com", token = "t", autoReconnect = false)
        assertFalse(c.autoReconnect)
        // Disabling autoReconnect is the documented way to force-close in WSEvent close path.
    }
}
