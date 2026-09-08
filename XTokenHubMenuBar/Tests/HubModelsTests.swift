import Foundation
import Testing
@testable import XTokenHubMenuBar

/// 用真实形状的后端 JSON 验证模型解码。
struct HubModelsDecodingTests {
    @Test func decodesSummaryEnvelope() throws {
        let json = """
        {"code":0,"message":"ok","data":{"total_requests":12,"success_requests":11,"error_requests":1,
        "prompt_tokens":1000,"completion_tokens":2000,"total_tokens":3000,"cached_tokens":400,
        "cache_hit_rate":0.4,"avg_duration_ms":1234.5,"native_ratio":0.9}}
        """
        let env = try JSONDecoder().decode(Envelope<Summary>.self, from: Data(json.utf8))
        #expect(env.code == 0)
        let s = try #require(env.data)
        #expect(s.totalRequests == 12)
        #expect(s.errorRequests == 1)
        #expect(s.totalTokens == 3000)
        #expect(s.avgDurationMS == 1234.5)
    }

    @Test func decodesTrendPointWithTs() throws {
        let json = """
        {"date":"2026-09-08","ts":1757299200,"requests":5,"total_tokens":12345,"error_requests":0}
        """
        let p = try JSONDecoder().decode(TrendPoint.self, from: Data(json.utf8))
        #expect(p.pointDate == Date(timeIntervalSince1970: 1_757_299_200))
        #expect(p.totalTokens == 12_345)
    }

    @Test func decodesTrendPointWithoutTs() throws {
        let json = """
        {"date":"2026-09-08","requests":5,"total_tokens":12345,"error_requests":0}
        """
        let p = try JSONDecoder().decode(TrendPoint.self, from: Data(json.utf8))
        #expect(p.id == "2026-09-08")
    }

    @Test func decodesRequestLogWithFractionalRFC3339() throws {
        let json = """
        {"id":7,"req_id":0,"created_at":"2026-09-08T12:34:56.123456+08:00","protocol":"chat_completions",
        "forward_mode":"native_passthrough","channel_id":1,"channel_name":"DeepSeek","key_id":2,
        "key_name":"claude","model":"deepseek-chat","stream":true,"prompt_tokens":10,"completion_tokens":20,
        "total_tokens":30,"cached_tokens":5,"cache_write_tokens":0,"cache_hit_rate":0.5,"duration_ms":850,
        "upstream_status":200,"client_ip":"127.0.0.1","user_agent":"test","session_id":"s",
        "request_headers":"{}","error":""}
        """
        let log = try JSONDecoder().decode(RequestLog.self, from: Data(json.utf8))
        #expect(log.logID == 7)
        #expect(log.reqID == 0)
        #expect(!log.isPending)
        #expect(!log.isError)
        #expect(log.loggedAt != nil)
        #expect(log.durationMS == 850)
    }

    @Test func decodesErrorRequestLog() throws {
        let json = """
        {"id":8,"req_id":0,"created_at":"2026-09-08T12:34:56Z","protocol":"messages","forward_mode":"converted",
        "channel_id":1,"channel_name":"DeepSeek","key_id":0,"key_name":"","model":"deepseek-chat","stream":false,
        "prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"cached_tokens":0,"cache_write_tokens":0,
        "cache_hit_rate":0,"duration_ms":40,"upstream_status":429,"client_ip":"","user_agent":"","session_id":"",
        "request_headers":"","error":"rate limited"}
        """
        let log = try JSONDecoder().decode(RequestLog.self, from: Data(json.utf8))
        #expect(log.isError)
    }

    @Test func decodesPageData() throws {
        let json = """
        {"code":0,"message":"ok","data":{"items":[{"id":1,"req_id":0,"created_at":"2026-09-08T00:00:00Z",
        "protocol":"chat_completions","forward_mode":"native_passthrough","channel_id":0,"channel_name":"",
        "key_id":0,"key_name":"","model":"gpt-5","stream":false,"prompt_tokens":0,"completion_tokens":0,
        "total_tokens":0,"cached_tokens":0,"cache_write_tokens":0,"cache_hit_rate":0,"duration_ms":0,
        "upstream_status":200,"client_ip":"","user_agent":"","session_id":"","request_headers":"","error":""}],
        "total":1,"page":1,"per_page":30}}
        """
        let env = try JSONDecoder().decode(Envelope<PageData<RequestLog>>.self, from: Data(json.utf8))
        let page = try #require(env.data)
        #expect(page.total == 1)
        #expect(page.perPage == 30)
        #expect(page.items.count == 1)
    }
}

/// WS 信封与各事件类型的解码。
struct WsDecodingTests {
    @Test func decodesThroughput() {
        let event = WsDecoder.decode(
            #"{"type":"stats.throughput","payload":{"tokens_per_sec":830.5,"active_streams":3},"ts":1757300000000}"#
        )
        #expect(event == .throughput(tokensPerSec: 830.5, activeStreams: 3))
    }

    @Test func decodesRequestStartedPendingRow() throws {
        let json = """
        {"type":"request.started","ts":1,"payload":{"id":0,"req_id":42,"created_at":"2026-09-08T12:00:00Z",
        "protocol":"messages","forward_mode":"converted","channel_id":0,"channel_name":"","key_id":1,
        "key_name":"claude-code","model":"gpt-5","stream":true,"prompt_tokens":0,"completion_tokens":0,
        "total_tokens":0,"cached_tokens":0,"cache_write_tokens":0,"cache_hit_rate":0,"duration_ms":0,
        "upstream_status":0,"client_ip":"","user_agent":"","session_id":"","request_headers":"","error":""}}
        """
        guard case .requestStarted(let log) = WsDecoder.decode(json) else {
            Issue.record("应为 requestStarted 事件")
            return
        }
        #expect(log.isPending)
        #expect(log.reqID == 42)
        #expect(log.stream)
    }

    @Test func decodesRequestCompleted() throws {
        let json = """
        {"type":"request.completed","ts":2,"payload":{"id":99,"req_id":42,"created_at":"2026-09-08T12:00:01Z",
        "protocol":"messages","forward_mode":"converted","channel_id":0,"channel_name":"","key_id":1,
        "key_name":"claude-code","model":"gpt-5","stream":true,"prompt_tokens":100,"completion_tokens":900,
        "total_tokens":1000,"cached_tokens":50,"cache_write_tokens":0,"cache_hit_rate":0.5,"duration_ms":3200,
        "upstream_status":200,"client_ip":"","user_agent":"","session_id":"","request_headers":"","error":""}}
        """
        guard case .requestCompleted(let log) = WsDecoder.decode(json) else {
            Issue.record("应为 requestCompleted 事件")
            return
        }
        #expect(!log.isPending)
        #expect(log.reqID == 42)
        #expect(log.totalTokens == 1000)
    }

    @Test func statsUpdatedWithNullPayload() {
        #expect(WsDecoder.decode(#"{"type":"stats.updated","payload":null,"ts":1}"#) == .statsUpdated)
    }

    @Test func pongIsIgnored() {
        #expect(WsDecoder.decode(#"{"type":"pong","payload":null,"ts":1}"#) == .ignored("pong"))
    }

    @Test func channelEventsMapToChannelUpdated() {
        #expect(WsDecoder.decode(#"{"type":"channel.status_changed","payload":{"id":1},"ts":1}"#) == .channelUpdated)
    }

    @Test func malformedInputReturnsNil() {
        #expect(WsDecoder.decode("not json") == nil)
        #expect(WsDecoder.decode(#"{"payload":{}}"#) == nil)
    }
}

struct TokenFormatterTests {
    /// 基准与 Web 端 compactCN 对齐(亿/万,≤2 位小数去尾零)。
    @Test func compact() {
        #expect(TokenFormatter.compact(0) == "0")
        #expect(TokenFormatter.compact(980) == "980")
        #expect(TokenFormatter.compact(1_234) == "1234")
        #expect(TokenFormatter.compact(12_345) == "1.23万")
        #expect(TokenFormatter.compact(123_456) == "12.35万")
        #expect(TokenFormatter.compact(3_400_000) == "340万")
        #expect(TokenFormatter.compact(48_787_304) == "4878.73万")
        #expect(TokenFormatter.compact(123_456_789) == "1.23亿")
        #expect(TokenFormatter.compact(1_050_000_000) == "10.5亿")
        #expect(TokenFormatter.compact(-2_500) == "-2500")
        #expect(TokenFormatter.compact(-25_000) == "-2.5万")
    }

    /// 对应 Web 端 compactCN(Math.round(tps))。
    @Test func compactSpeed() {
        #expect(TokenFormatter.compactSpeed(0) == "0")
        #expect(TokenFormatter.compactSpeed(0.4) == "0")
        #expect(TokenFormatter.compactSpeed(830.5) == "831")
        #expect(TokenFormatter.compactSpeed(1234.6) == "1235")
    }

    /// 对应 Web 端 duration():<1s 显示 ms,否则 2 位小数的秒。
    @Test func duration() {
        #expect(TokenFormatter.duration(780) == "780ms")
        #expect(TokenFormatter.duration(12_400) == "12.40s")
        #expect(TokenFormatter.duration(1_250_000) == "1250.00s")
    }
}

struct HubURLTests {
    @Test func normalizesBase() {
        #expect(HubURL.httpBase("  127.0.0.1:9192/ ") == URL(string: "http://127.0.0.1:9192"))
        #expect(HubURL.httpBase("https://hub.example.com") == URL(string: "https://hub.example.com"))
        #expect(HubURL.httpBase("") == HubURL.fallback)
        #expect(HubURL.httpBase("::::") == HubURL.fallback)
    }

    @Test func buildsWsURL() {
        #expect(HubURL.wsBase(URL(string: "http://127.0.0.1:9192")!).absoluteString == "ws://127.0.0.1:9192/api/v1/ws")
        #expect(HubURL.wsBase(URL(string: "https://hub.example.com")!).absoluteString == "wss://hub.example.com/api/v1/ws")
    }
}
