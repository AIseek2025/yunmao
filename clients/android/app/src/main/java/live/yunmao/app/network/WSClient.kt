package live.yunmao.app.network

import io.ktor.client.HttpClient
import io.ktor.client.plugins.websocket.WebSockets
import io.ktor.client.plugins.websocket.wss
import io.ktor.websocket.Frame
import io.ktor.websocket.WebSocketSession
import io.ktor.websocket.readText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

sealed class WSEvent {
    data class FeedStateChange(val roomId: String, val payload: JsonObject) : WSEvent()
    data class ChatMessage(val data: JsonObject) : WSEvent()
    data class ChatModeration(val messageId: String, val action: String) : WSEvent()
    data class Reconnected(val attempt: Int) : WSEvent()
    data class Disconnected(val cause: Throwable?) : WSEvent()
    data class Unknown(val type: String, val payload: JsonObject) : WSEvent()
}

/**
 * Android WSClient with exponential-backoff auto-reconnect.
 *
 * Mirrors the iOS `WSClient` (see `clients/ios/.../Network/WSClient.swift`).
 * Subscribed room list is preserved across reconnects so the UI does not need to
 * re-issue subscribe calls when the socket is re-established after a network churn
 * or gateway failover.
 */
class WSClient(
    private val wsBase: String,
    private val token: String,
    var autoReconnect: Boolean = true,
    var baseBackoffMs: Long = 1_000L,
    var maxBackoffMs: Long = 30_000L,
    var maxAttempts: Int = 5,
) {
    private val events = Channel<WSEvent>(Channel.BUFFERED)
    fun events(): Flow<WSEvent> = events.receiveAsFlow()

    @Volatile private var session: WebSocketSession? = null
    private val subscribedRooms = LinkedHashSet<String>()
    private var connectJob: Job? = null

    suspend fun connect(client: HttpClient) {
        val urlNoScheme = wsBase.removePrefix("wss://").removePrefix("ws://")
        client.wss(host = urlNoScheme, path = "/ws") {
            session = this
            outgoing.send(Frame.Text("""{"type":"auth","token":"$token"}"""))
            for (frame in incoming) {
                val text = (frame as? Frame.Text)?.readText() ?: continue
                handle(text)
            }
        }
    }

    /**
     * Wraps [connect] with automatic retry on disconnect, using exponential backoff
     * (1s, 2s, 4s, 8s, 16s, … capped at [maxBackoffMs]), up to [maxAttempts] tries.
     * After each successful reconnect the cached [subscribedRooms] are re-subscribed.
     */
    suspend fun connectWithRetry(client: HttpClient) {
        var attempt = 0
        var backoff = baseBackoffMs
        while (true) {
            try {
                connect(client)
                // Re-establish subscriptions on a clean connect/reconnect.
                val rooms = synchronized(subscribedRooms) { subscribedRooms.toList() }
                for (r in rooms) {
                    session?.outgoing?.send(
                        Frame.Text("""{"type":"subscribe","room_id":"$r"}""")
                    )
                }
                if (attempt > 0) {
                    events.send(WSEvent.Reconnected(attempt))
                }
                return
            } catch (ce: CancellationException) {
                throw ce
            } catch (t: Throwable) {
                attempt++
                events.send(WSEvent.Disconnected(t))
                if (!autoReconnect || attempt >= maxAttempts) {
                    return
                }
                delay(backoff)
                backoff = (backoff * 2).coerceAtMost(maxBackoffMs)
            }
        }
    }

    suspend fun subscribe(roomId: String) {
        synchronized(subscribedRooms) { subscribedRooms.add(roomId) }
        session?.outgoing?.send(Frame.Text("""{"type":"subscribe","room_id":"$roomId"}"""))
    }

    suspend fun sendChat(roomId: String, body: String, clientId: String) {
        // 用 Json 库正确转义 body（含引号、控制字符），避免手拼 JSON 字符串。
        val payload = buildString {
            append("{\"type\":\"chat.send\",\"room_id\":\"")
            append(roomId)
            append("\",\"body\":")
            append(Json.encodeToString(kotlinx.serialization.builtins.serializer(), body))
            append(",\"client_msg_id\":\"")
            append(clientId)
            append("\"}")
        }
        session?.outgoing?.send(Frame.Text(payload))
    }

    private suspend fun handle(text: String) {
        runCatching {
            val obj = Json.parseToJsonElement(text).jsonObject
            val type = obj["type"]?.jsonPrimitive?.contentOrNull ?: "unknown"
            val ev: WSEvent = when (type) {
                "feed.state_change", "feed.created", "feed.completed" -> {
                    val roomId = obj["room_id"]?.jsonPrimitive?.contentOrNull ?: ""
                    WSEvent.FeedStateChange(roomId, obj)
                }
                "room.chat.message", "chat.message" -> WSEvent.ChatMessage(obj)
                "room.chat.moderation", "chat.moderation" -> {
                    val mid = obj["message_id"]?.jsonPrimitive?.contentOrNull ?: ""
                    val act = obj["action"]?.jsonPrimitive?.contentOrNull ?: "hide"
                    WSEvent.ChatModeration(mid, act)
                }
                else -> WSEvent.Unknown(type, obj)
            }
            events.send(ev)
        }
    }
}
