package live.yunmao.app.pay

import android.app.Activity
import com.alipay.sdk.app.PayTask
import com.tencent.mm.opensdk.modelpay.PayReq
import com.tencent.mm.opensdk.openapi.WXAPIFactory
import live.yunmao.app.BuildConfig
import live.yunmao.app.model.PrepayResponse

// PayManager：根据后端 prepay 响应调起微信 / 支付宝。
class PayManager(private val activity: Activity) {
    private val wxApi by lazy {
        WXAPIFactory.createWXAPI(activity, BuildConfig.WECHAT_APP_ID, true).also {
            it.registerApp(BuildConfig.WECHAT_APP_ID)
        }
    }

    /** 微信 Native 拉起。后端 prepay.client_hints 至少要包含：
     *  prepay_id, nonce_str, timestamp, sign, partner_id (mch_id)。
     */
    fun startWeChat(prepay: PrepayResponse): Boolean {
        val hints = prepay.clientHints
        val req = PayReq().apply {
            appId = BuildConfig.WECHAT_APP_ID
            partnerId = hints["partner_id"] ?: ""
            prepayId = prepay.prepayId.removePrefix("wxpay_real_").ifEmpty { prepay.prepayId }
            nonceStr = hints["nonce_str"] ?: ""
            timeStamp = hints["timestamp"] ?: ""
            packageValue = hints["package"] ?: "Sign=WXPay"
            sign = hints["sign"] ?: ""
        }
        return wxApi.sendReq(req)
    }

    /** 支付宝拉起：后端 prepay.pay_url 是已签名的 orderString（app 模式）。 */
    fun startAlipay(prepay: PrepayResponse, callback: (Map<String, String>) -> Unit) {
        val orderString = prepay.payUrl ?: ""
        Thread {
            val result = PayTask(activity).payV2(orderString, true)
            callback(result)
        }.start()
    }
}
