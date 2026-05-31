# 保留 Kotlinx Serialization 元数据
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.AnnotationsKt
-keep,includedescriptorclasses class live.yunmao.app.**$$serializer { *; }
-keepclassmembers class live.yunmao.app.** {
    *** Companion;
}

# WebRTC
-keep class org.webrtc.** { *; }
-keep class io.github.webrtc.** { *; }

# 微信 / 支付宝
-keep class com.tencent.mm.opensdk.** { *; }
-keep class com.alipay.sdk.** { *; }
