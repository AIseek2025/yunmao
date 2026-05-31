package live.yunmao.app.webrtc

import android.content.Context
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.webrtc.IceCandidate
import org.webrtc.MediaConstraints
import org.webrtc.MediaStream
import org.webrtc.PeerConnection
import org.webrtc.PeerConnectionFactory
import org.webrtc.RtpReceiver
import org.webrtc.SdpObserver
import org.webrtc.SessionDescription
import org.webrtc.SurfaceViewRenderer

class WhepClient(
    context: Context,
) {
    private val factory: PeerConnectionFactory
    private var pc: PeerConnection? = null
    private val http = OkHttpClient.Builder().build()

    init {
        PeerConnectionFactory.initialize(
            PeerConnectionFactory.InitializationOptions.builder(context).createInitializationOptions()
        )
        factory = PeerConnectionFactory.builder().createPeerConnectionFactory()
    }

    fun start(
        whepUrl: String,
        token: String,
        iceServers: List<PeerConnection.IceServer>,
        renderer: SurfaceViewRenderer,
        onError: (Throwable) -> Unit,
    ) {
        val config = PeerConnection.RTCConfiguration(iceServers).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
        }
        val observer = object : PeerConnection.Observer {
            override fun onSignalingChange(state: PeerConnection.SignalingState?) {}
            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {}
            override fun onIceConnectionReceivingChange(p0: Boolean) {}
            override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {}
            override fun onIceCandidate(c: IceCandidate?) {}
            override fun onIceCandidatesRemoved(p0: Array<out IceCandidate>?) {}
            override fun onAddStream(p0: MediaStream?) {}
            override fun onRemoveStream(p0: MediaStream?) {}
            override fun onDataChannel(p0: org.webrtc.DataChannel?) {}
            override fun onRenegotiationNeeded() {}
            override fun onAddTrack(receiver: RtpReceiver?, mediaStreams: Array<out MediaStream>?) {
                val track = receiver?.track()
                if (track is org.webrtc.VideoTrack) {
                    track.addSink(renderer)
                }
            }
        }
        val pc = factory.createPeerConnection(config, observer) ?: run {
            onError(IllegalStateException("createPeerConnection returned null"))
            return
        }
        this.pc = pc
        pc.addTransceiver(
            org.webrtc.MediaStreamTrack.MediaType.MEDIA_TYPE_VIDEO,
            org.webrtc.RtpTransceiver.RtpTransceiverInit(org.webrtc.RtpTransceiverDirection.RECV_ONLY)
        )
        pc.addTransceiver(
            org.webrtc.MediaStreamTrack.MediaType.MEDIA_TYPE_AUDIO,
            org.webrtc.RtpTransceiver.RtpTransceiverInit(org.webrtc.RtpTransceiverDirection.RECV_ONLY)
        )
        val constraints = MediaConstraints().apply {
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveVideo", "true"))
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveAudio", "true"))
        }
        pc.createOffer(object : SimpleSdpObserver() {
            override fun onCreateSuccess(offer: SessionDescription) {
                pc.setLocalDescription(object : SimpleSdpObserver() {
                    override fun onSetSuccess() {
                        Thread {
                            try {
                                val req = Request.Builder().url(whepUrl)
                                    .addHeader("Content-Type", "application/sdp")
                                    .addHeader("Authorization", "Bearer $token")
                                    .post(offer.description.toRequestBody("application/sdp".toMediaTypeOrNull()))
                                    .build()
                                val resp = http.newCall(req).execute()
                                if (!resp.isSuccessful) {
                                    onError(IllegalStateException("WHEP ${resp.code}"))
                                    return@Thread
                                }
                                val answerSdp = resp.body!!.string()
                                val validation = SdpValidator.validateAnswer(answerSdp)
                                if (!validation.valid) {
                                    onError(IllegalStateException("Invalid WHEP answer SDP: ${validation.reason}"))
                                    return@Thread
                                }
                                val answer = SessionDescription(SessionDescription.Type.ANSWER, answerSdp)
                                pc.setRemoteDescription(SimpleSdpObserver(), answer)
                            } catch (e: Throwable) {
                                onError(e)
                            }
                        }.start()
                    }
                }, offer)
            }
        }, constraints)
    }

    fun close() {
        pc?.close()
        pc = null
    }
}

data class SdpValidationResult(val valid: Boolean, val reason: String = "")

object SdpValidator {
    fun validateAnswer(sdp: String): SdpValidationResult {
        if (sdp.isBlank()) return SdpValidationResult(false, "SDP body is empty")
        val lines = sdp.lines()
        val hasVersion = lines.any { it.startsWith("v=") }
        if (!hasVersion) return SdpValidationResult(false, "missing v= line")
        val hasOrigin = lines.any { it.startsWith("o=") }
        if (!hasOrigin) return SdpValidationResult(false, "missing o= line")
        val hasSessionName = lines.any { it.startsWith("s=") }
        if (!hasSessionName) return SdpValidationResult(false, "missing s= line")
        val hasMediaVideo = lines.any { it.startsWith("m=video") }
        val hasMediaAudio = lines.any { it.startsWith("m=audio") }
        if (!hasMediaVideo && !hasMediaAudio) return SdpValidationResult(false, "no media section found")
        return SdpValidationResult(true)
    }

    fun extractMediaTypes(sdp: String): List<String> {
        return sdp.lines()
            .filter { it.startsWith("m=") }
            .map { it.substring(2).split(" ").firstOrNull() ?: "" }
    }
}

open class SimpleSdpObserver : SdpObserver {
    override fun onCreateSuccess(p0: SessionDescription?) {}
    override fun onSetSuccess() {}
    override fun onCreateFailure(p0: String?) {}
    override fun onSetFailure(p0: String?) {}
}

    fun start(
        whepUrl: String,
        token: String,
        iceServers: List<PeerConnection.IceServer>,
        renderer: SurfaceViewRenderer,
        onError: (Throwable) -> Unit,
    ) {
        val config = PeerConnection.RTCConfiguration(iceServers).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
        }
        val observer = object : PeerConnection.Observer {
            override fun onSignalingChange(state: PeerConnection.SignalingState?) {}
            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {}
            override fun onIceConnectionReceivingChange(p0: Boolean) {}
            override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {}
            override fun onIceCandidate(c: IceCandidate?) {}
            override fun onIceCandidatesRemoved(p0: Array<out IceCandidate>?) {}
            override fun onAddStream(p0: MediaStream?) {}
            override fun onRemoveStream(p0: MediaStream?) {}
            override fun onDataChannel(p0: org.webrtc.DataChannel?) {}
            override fun onRenegotiationNeeded() {}
            override fun onAddTrack(receiver: RtpReceiver?, mediaStreams: Array<out MediaStream>?) {
                val track = receiver?.track()
                if (track is org.webrtc.VideoTrack) {
                    track.addSink(renderer)
                }
            }
        }
        val pc = factory.createPeerConnection(config, observer) ?: run {
            onError(IllegalStateException("createPeerConnection returned null"))
            return
        }
        this.pc = pc
        pc.addTransceiver(
            org.webrtc.MediaStreamTrack.MediaType.MEDIA_TYPE_VIDEO,
            org.webrtc.RtpTransceiver.RtpTransceiverInit(org.webrtc.RtpTransceiver.RtpTransceiverDirection.RECV_ONLY)
        )
        pc.addTransceiver(
            org.webrtc.MediaStreamTrack.MediaType.MEDIA_TYPE_AUDIO,
            org.webrtc.RtpTransceiver.RtpTransceiverInit(org.webrtc.RtpTransceiver.RtpTransceiverDirection.RECV_ONLY)
        )
        val constraints = MediaConstraints().apply {
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveVideo", "true"))
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveAudio", "true"))
        }
        pc.createOffer(object : SimpleSdpObserver() {
            override fun onCreateSuccess(offer: SessionDescription) {
                pc.setLocalDescription(object : SimpleSdpObserver() {
                    override fun onSetSuccess() {
                        Thread {
                            try {
                                val req = Request.Builder().url(whepUrl)
                                    .addHeader("Content-Type", "application/sdp")
                                    .addHeader("Authorization", "Bearer $token")
                                    .post(offer.description.toRequestBody("application/sdp".toMediaTypeOrNull()))
                                    .build()
                                val resp = http.newCall(req).execute()
                                if (!resp.isSuccessful) {
                                    onError(IllegalStateException("WHEP ${'$'}{resp.code}"))
                                    return@Thread
                                }
                                val answer = SessionDescription(SessionDescription.Type.ANSWER, resp.body!!.string())
                                pc.setRemoteDescription(SimpleSdpObserver(), answer)
                            } catch (e: Throwable) {
                                onError(e)
                            }
                        }.start()
                    }
                }, offer)
            }
        }, constraints)
    }

    fun close() {
        pc?.close()
        pc = null
    }
}

open class SimpleSdpObserver : SdpObserver {
    override fun onCreateSuccess(p0: SessionDescription?) {}
    override fun onSetSuccess() {}
    override fun onCreateFailure(p0: String?) {}
    override fun onSetFailure(p0: String?) {}
}
