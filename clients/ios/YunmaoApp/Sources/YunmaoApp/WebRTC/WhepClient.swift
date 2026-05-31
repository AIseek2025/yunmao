// WhepClient：iOS WebRTC WHEP 拉流。
//
// 流程：
//   1. GET /v1/rooms/{id}/ice-servers      → IceServers
//   2. 创建 RTCPeerConnection（addTransceiver recvonly H.264 + Opus）
//   3. createOffer → setLocalDescription
//   4. POST /whep/{room_id} (application/sdp) → 收 answer
//   5. setRemoteDescription → 等 ICE/DTLS 完成
//   6. 渲染远端 video/audio track 到 RTCVideoRenderer。

import Foundation

public struct SdpValidationResult: Equatable {
    public let valid: Bool
    public let reason: String
    public init(valid: Bool, reason: String = "") {
        self.valid = valid
        self.reason = reason
    }
}

public enum SdpValidator {
    public static func validateAnswer(_ sdp: String) -> SdpValidationResult {
        if sdp.isEmpty { return SdpValidationResult(valid: false, reason: "SDP body is empty") }
        let lines = sdp.components(separatedBy: "\n")
        if !lines.contains(where: { $0.hasPrefix("v=") }) {
            return SdpValidationResult(valid: false, reason: "missing v= line")
        }
        if !lines.contains(where: { $0.hasPrefix("o=") }) {
            return SdpValidationResult(valid: false, reason: "missing o= line")
        }
        if !lines.contains(where: { $0.hasPrefix("s=") }) {
            return SdpValidationResult(valid: false, reason: "missing s= line")
        }
        let hasMedia = lines.contains(where: { $0.hasPrefix("m=video") || $0.hasPrefix("m=audio") })
        if !hasMedia {
            return SdpValidationResult(valid: false, reason: "no media section found")
        }
        return SdpValidationResult(valid: true)
    }

    public static func extractMediaTypes(_ sdp: String) -> [String] {
        sdp.components(separatedBy: "\n")
            .filter { $0.hasPrefix("m=") }
            .compactMap { line in
                let parts = line.dropFirst(2).split(separator: " ", maxSplits: 1)
                return parts.first.map(String.init)
            }
    }
}

#if canImport(WebRTC)
import WebRTC

public final class WhepClient: NSObject, RTCPeerConnectionDelegate {
    private let factory: RTCPeerConnectionFactory
    private var pc: RTCPeerConnection?
    public weak var videoRenderer: RTCVideoRenderer?

    public init(factory: RTCPeerConnectionFactory = .init()) {
        self.factory = factory
        super.init()
    }

    public func start(whepURL: URL, token: String, iceServers: [IceServersResponse.IceServer]) async throws {
        let config = RTCConfiguration()
        config.iceServers = iceServers.map { srv in
            RTCIceServer(urlStrings: srv.urls,
                         username: srv.username,
                         credential: srv.credential)
        }
        config.sdpSemantics = .unifiedPlan
        config.bundlePolicy = .maxBundle
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        guard let pc = factory.peerConnection(with: config, constraints: constraints, delegate: self) else {
            throw NSError(domain: "whep", code: -1)
        }
        self.pc = pc

        let videoInit = RTCRtpTransceiverInit()
        videoInit.direction = .recvOnly
        _ = pc.addTransceiver(of: .video, init: videoInit)

        let audioInit = RTCRtpTransceiverInit()
        audioInit.direction = .recvOnly
        _ = pc.addTransceiver(of: .audio, init: audioInit)

        let offerConstraints = RTCMediaConstraints(mandatoryConstraints: [
            "OfferToReceiveAudio": "true",
            "OfferToReceiveVideo": "true",
        ], optionalConstraints: nil)
        let offer = try await withCheckedThrowingContinuation { (cont: CheckedContinuation<RTCSessionDescription, Error>) in
            pc.offer(for: offerConstraints) { sdp, error in
                if let error { cont.resume(throwing: error); return }
                guard let sdp else { cont.resume(throwing: NSError(domain: "whep", code: -2)); return }
                cont.resume(returning: sdp)
            }
        }
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Void, Error>) in
            pc.setLocalDescription(offer) { error in
                if let error { cont.resume(throwing: error) } else { cont.resume() }
            }
        }

        var req = URLRequest(url: whepURL)
        req.httpMethod = "POST"
        req.setValue("application/sdp", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.httpBody = offer.sdp.data(using: .utf8)
        let (data, resp) = try await URLSession.shared.data(for: req)
        guard let http = resp as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw NSError(domain: "whep", code: (resp as? HTTPURLResponse)?.statusCode ?? -3,
                          userInfo: ["body": String(data: data, encoding: .utf8) ?? ""])
        }
        guard let answerSDP = String(data: data, encoding: .utf8) else {
            throw NSError(domain: "whep", code: -4)
        }
        let validation = SdpValidator.validateAnswer(answerSDP)
        if !validation.valid {
            throw NSError(domain: "whep", code: -5,
                          userInfo: ["reason": validation.reason])
        }
        let answer = RTCSessionDescription(type: .answer, sdp: answerSDP)
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Void, Error>) in
            pc.setRemoteDescription(answer) { error in
                if let error { cont.resume(throwing: error) } else { cont.resume() }
            }
        }
    }

    public func close() {
        pc?.close()
        pc = nil
    }

    // MARK: - RTCPeerConnectionDelegate

    public func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {}
    public func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
    public func peerConnection(_ peerConnection: RTCPeerConnection, didAdd rtpReceiver: RTCRtpReceiver, streams mediaStreams: [RTCMediaStream]) {
        if let track = rtpReceiver.track as? RTCVideoTrack, let renderer = videoRenderer {
            track.add(renderer)
        }
    }
}
#endif
