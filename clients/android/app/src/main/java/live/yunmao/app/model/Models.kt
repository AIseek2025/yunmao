package live.yunmao.app.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class AuthToken(
    val token: String,
    @SerialName("user_id") val userId: String,
    @SerialName("expires_at") val expiresAt: Long,
) {
    val isExpired: Boolean
        get() = System.currentTimeMillis() / 1000 > expiresAt - 30
}

@Serializable
data class Room(
    val id: String,
    val name: String,
    val status: String,
    val cover: String? = null,
    @SerialName("protocol_pref") val protocolPref: String? = null,
    @SerialName("webrtc_eligible") val webrtcEligible: Boolean? = null,
)

@Serializable
data class RoomList(val rooms: List<Room>)

@Serializable
data class RoomSubscription(
    @SerialName("room_id") val roomId: String,
    val token: String,
    @SerialName("url_playback") val urlPlayback: String,
    @SerialName("url_whep") val urlWhep: String? = null,
    @SerialName("webrtc_enabled") val webrtcEnabled: Boolean = false,
)

@Serializable
data class FeedRequest(
    @SerialName("room_id") val roomId: String,
    @SerialName("user_id") val userId: String,
    val grams: Int,
    @SerialName("feed_ticket_id") val feedTicketId: String,
    @SerialName("idempotency_key") val idempotencyKey: String,
)

@Serializable
data class FeedResponse(
    val id: String,
    val status: String,
    @SerialName("cam_record_url") val camRecordUrl: String? = null,
)

@Serializable
data class Wallet(
    @SerialName("user_id") val userId: String,
    @SerialName("balance_fen") val balanceFen: Long,
    val coins: Long,
)

@Serializable
data class PrepayResponse(
    val channel: String,
    @SerialName("prepay_id") val prepayId: String,
    @SerialName("pay_url") val payUrl: String? = null,
    @SerialName("qr_content") val qrContent: String? = null,
    @SerialName("client_hints") val clientHints: Map<String, String> = emptyMap(),
)

@Serializable
data class ChatMessage(
    val id: String,
    @SerialName("room_id") val roomId: String,
    @SerialName("user_id") val userId: String,
    val nickname: String? = null,
    val body: String,
    @SerialName("created_at") val createdAt: Long,
    var moderation: String? = null,
)
