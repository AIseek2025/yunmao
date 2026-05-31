package live.yunmao.app

import kotlinx.serialization.json.Json
import live.yunmao.app.model.PrepayResponse
import org.junit.Assert.assertEquals
import org.junit.Test

class PrepayParseTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test fun parseWeChatPrepay() {
        val src = """
        {
          "channel":"wechatpay",
          "prepay_id":"wxpay_real_1234",
          "client_hints":{
            "nonce_str":"abc","timestamp":"1700000000","sign":"deadbeef",
            "partner_id":"169123456","package":"Sign=WXPay"
          }
        }
        """.trimIndent()
        val resp = json.decodeFromString(PrepayResponse.serializer(), src)
        assertEquals("wechatpay", resp.channel)
        assertEquals("wxpay_real_1234", resp.prepayId)
        assertEquals("169123456", resp.clientHints["partner_id"])
    }

    @Test fun parseAlipayPrepay() {
        val src = """
        {
          "channel":"alipay",
          "prepay_id":"alipay_real_56789",
          "pay_url":"app_id=2021000&biz_content=...&sign=xxx",
          "client_hints":{"order_string":"app_id=..."}
        }
        """.trimIndent()
        val resp = json.decodeFromString(PrepayResponse.serializer(), src)
        assertEquals("alipay", resp.channel)
        assertEquals(true, resp.payUrl?.contains("biz_content"))
    }
}
