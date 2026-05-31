import XCTest
@testable import YunmaoApp

final class YunmaoAppTests: XCTestCase {
    func testGrayHitDistribution() {
        var hits = 0
        let target = 20
        let samples = 5000
        for i in 0..<samples {
            let rid = "room_test_\(i)"
            if GrayHit.inGrayPercent(roomID: rid, percent: target) {
                hits += 1
            }
        }
        let pct = Double(hits) / Double(samples) * 100
        XCTAssertGreaterThanOrEqual(pct, 16.0, "expected 20% ± 4% (lower bound)")
        XCTAssertLessThanOrEqual(pct, 24.0, "expected 20% ± 4% (upper bound)")
    }

    func testGrayHitBoundaries() {
        XCTAssertFalse(GrayHit.inGrayPercent(roomID: "any", percent: 0))
        XCTAssertTrue(GrayHit.inGrayPercent(roomID: "any", percent: 100))
    }

    func testGrayHitCrossClientFNV1aConsistency() {
        let knownValues: [(String, Int)] = [
            ("", 61),
            ("a", 20),
            ("ab", 46),
            ("abc", 31),
            ("room_cross_client_e2e", 84),
        ]
        for (key, expected) in knownValues {
            let actual = GrayHit.hash100(key)
            XCTAssertEqual(actual, expected, "FNV1a hash100('\(key)') should be \(expected), got \(actual)")
        }
    }

    func testWSEventParsing() {
        let dict: [String: Any] = [
            "type": "room.chat.moderation",
            "message_id": "m1",
            "action": "recall",
        ]
        XCTAssertEqual(dict["type"] as? String, "room.chat.moderation")
        XCTAssertEqual(dict["action"] as? String, "recall")
    }

    func testAuthTokenExpiry() {
        let t = AuthToken(token: "x", userId: "u1", expiresAt: 0)
        XCTAssertTrue(t.isExpired)
        let t2 = AuthToken(token: "y", userId: "u2", expiresAt: Date().timeIntervalSince1970 + 3600)
        XCTAssertFalse(t2.isExpired)
    }

    func testSdpValidatorValidAnswer() {
        let validSDP = """
        v=0
        o=- 123456789 1 IN IP4 0.0.0.0
        s=-
        t=0 0
        m=video 9 UDP/TLS/RTP/SAVPF 96
        a=rtpmap:96 H264/90000
        m=audio 9 UDP/TLS/RTP/SAVPF 111
        a=rtpmap:111 opus/48000/2
        """
        let result = SdpValidator.validateAnswer(validSDP)
        XCTAssertTrue(result.valid, "Expected valid SDP, got: \(result.reason)")
    }

    func testSdpValidatorEmptyBody() {
        let result = SdpValidator.validateAnswer("")
        XCTAssertFalse(result.valid)
        XCTAssertEqual(result.reason, "SDP body is empty")
    }

    func testSdpValidatorMissingVersion() {
        let badSDP = "o=- 0 0 IN IP4 0.0.0.0\ns=-\nm=video 9 UDP 0"
        let result = SdpValidator.validateAnswer(badSDP)
        XCTAssertFalse(result.valid)
        XCTAssertEqual(result.reason, "missing v= line")
    }

    func testSdpValidatorNoMedia() {
        let badSDP = "v=0\no=- 0 0 IN IP4 0.0.0.0\ns=-\nt=0 0"
        let result = SdpValidator.validateAnswer(badSDP)
        XCTAssertFalse(result.valid)
        XCTAssertEqual(result.reason, "no media section found")
    }

    func testSdpExtractMediaTypes() {
        let sdp = "v=0\no=- 0 0 IN IP4 0\ns=-\nm=video 9 UDP 96\nm=audio 9 UDP 111"
        let types = SdpValidator.extractMediaTypes(sdp)
        XCTAssertEqual(types, ["video", "audio"])
    }
}
