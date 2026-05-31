package live.yunmao.app.network

import io.ktor.client.HttpClient
import io.ktor.client.engine.HttpClientEngine
import io.ktor.client.engine.okhttp.OkHttp
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.websocket.WebSockets
import io.ktor.client.request.bearerAuth
import io.ktor.client.request.get
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.client.call.body
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.serialization.kotlinx.json.json
import kotlinx.serialization.json.Json
import live.yunmao.app.model.*

// YunmaoApi：基于 Ktor 的 HTTP 客户端，统一注入 Bearer JWT。
// 与 iOS YunmaoAPI / Web YunmaoApi 路径一致。
class YunmaoApi(
    private val baseUrl: String,
    private val tokenProvider: () -> String?,
    engine: HttpClientEngine = OkHttp.create(),
) {
    private val client = HttpClient(engine) {
        install(ContentNegotiation) {
            json(Json {
                ignoreUnknownKeys = true
                explicitNulls = false
                encodeDefaults = true
            })
        }
        install(WebSockets)
    }

    private fun io.ktor.client.request.HttpRequestBuilder.applyAuth() {
        tokenProvider()?.let { bearerAuth(it) }
    }

    private suspend inline fun <reified T> get(path: String): T =
        client.get("$baseUrl$path") {
            tokenProvider()?.let { bearerAuth(it) }
        }.body()

    private suspend inline fun <reified Req, reified Resp> postJson(path: String, body: Req): Resp =
        client.post("$baseUrl$path") {
            tokenProvider()?.let { bearerAuth(it) }
            contentType(ContentType.Application.Json)
            setBody(body)
        }.body()

    suspend fun login(phone: String, code: String): AuthToken =
        postJson("/api/v1/auth/login", mapOf("phone" to phone, "code" to code))

    suspend fun listRooms(): List<Room> = get<RoomList>("/api/v1/rooms").rooms

    suspend fun subscription(roomId: String): RoomSubscription =
        get("/api/v1/rooms/$roomId/subscription")

    suspend fun feed(req: FeedRequest): FeedResponse = postJson("/api/v1/feed", req)

    suspend fun wallet(userId: String): Wallet = get("/api/v1/wallets/$userId")

    suspend fun createPrepay(orderId: String, channel: String, amountFen: Long): PrepayResponse =
        client.post("$baseUrl/api/v1/orders/$orderId/prepay?channel=$channel") {
            tokenProvider()?.let { bearerAuth(it) }
            contentType(ContentType.Application.Json)
            setBody(mapOf("amount_fen" to amountFen, "subject" to "yunmao 充值"))
        }.body()

    fun close() = client.close()
}
