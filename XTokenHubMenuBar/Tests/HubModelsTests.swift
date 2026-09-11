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

    /// 计费三字段(Web 端花费列与计价口径提示依赖)跟随实时/列表载荷下发。
    @Test func decodesRequestLogCostFields() throws {
        let json = """
        {"id":11,"req_id":0,"created_at":"2026-09-11T12:00:00Z","protocol":"chat_completions",
        "forward_mode":"native_passthrough","channel_id":1,"channel_name":"DeepSeek","key_id":2,
        "key_name":"claude","model":"deepseek-chat","stream":true,"prompt_tokens":1000,
        "completion_tokens":500,"total_tokens":1500,"cached_tokens":800,"cache_write_tokens":0,
        "cache_hit_rate":0.8,"cost_usd":0.0042,"usage_style":"openai","price_period":"off_peak",
        "duration_ms":1200,"upstream_status":200,"client_ip":"127.0.0.1","user_agent":"claude-cli/1.0.0",
        "session_id":"s-1","request_headers":"{}","error":""}
        """
        let log = try JSONDecoder().decode(RequestLog.self, from: Data(json.utf8))
        #expect(log.costUSD == 0.0042)
        #expect(log.usageStyle == "openai")
        #expect(log.pricePeriod == "off_peak")
        #expect(log.hasCacheHit)
        #expect(log.protocolLabel == "chat")
        #expect(log.modeLabel == "透传")
        #expect(log.clientLabel == "Claude Code/1.0.0")
        #expect(log.tokenInOutText == "↑1000 ↓500")
        #expect(log.costText(money: .usd) == "$0.0042")
    }

    /// 旧后端/进行中行缺计费键:解码不失败,按缺省值展示。
    @Test func decodesRequestLogWithoutCostFields() throws {
        let json = """
        {"id":12,"req_id":0,"created_at":"2026-09-11T12:00:00Z","protocol":"chat_completions",
        "forward_mode":"","channel_id":0,"channel_name":"","key_id":0,"key_name":"","model":"gpt-5",
        "stream":false,"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"cached_tokens":0,
        "cache_write_tokens":0,"cache_hit_rate":0,"duration_ms":0,"upstream_status":200,"client_ip":"",
        "user_agent":"","session_id":"","request_headers":"","error":""}
        """
        let log = try JSONDecoder().decode(RequestLog.self, from: Data(json.utf8))
        #expect(log.costUSD == nil)
        #expect(log.usageStyle == nil)
        #expect(log.pricePeriod == nil)
        #expect(!log.hasCacheHit)
        #expect(log.tokenInOutText == "↑0 ↓0")
        #expect(log.costText(money: .usd) == "$0")
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

    /// 对应 Web 端 percent():默认 1 位小数,可指定位数。
    @Test func percent() {
        #expect(TokenFormatter.percent(0) == "0.0%")
        #expect(TokenFormatter.percent(0.234) == "23.4%")
        #expect(TokenFormatter.percent(1) == "100.0%")
        #expect(TokenFormatter.percent(0.62, digits: 0) == "62%")
        #expect(TokenFormatter.percent(.nan) == "0%")
    }

    /// 对应 Web 端 tokenSpeed():completion/耗时,进行中(0)显示 —。
    @Test func speed() {
        #expect(TokenFormatter.speed(completionTokens: 900, durationMS: 3_000) == "300 tok/s")
        #expect(TokenFormatter.speed(completionTokens: 500, durationMS: 1_200) == "417 tok/s")
        #expect(TokenFormatter.speed(completionTokens: 0, durationMS: 1_200) == "—")
        #expect(TokenFormatter.speed(completionTokens: 500, durationMS: 0) == "—")
        // 大数走 compact 单位
        #expect(TokenFormatter.speed(completionTokens: 50_000, durationMS: 1_000) == "5万 tok/s")
    }

    /// 统计磁贴把数量级后缀拆出来单独排版,故只需切出末尾的 万/亿。
    @Test func splitCompact() {
        let plain = TokenFormatter.splitCompact(980)
        #expect(plain.number == "980")
        #expect(plain.unit == "")

        let wan = TokenFormatter.splitCompact(92_904_700)
        #expect(wan.number == "9290.47")
        #expect(wan.unit == "万")

        let yi = TokenFormatter.splitCompact(435_000_000)
        #expect(yi.number == "4.35")
        #expect(yi.unit == "亿")

        // 负值(理论上不会出现,但不能把符号切到单位里去)
        let negative = TokenFormatter.splitCompact(-25_000)
        #expect(negative.number == "-2.5")
        #expect(negative.unit == "万")

        // 边界:恰好跨过 1 万门槛、逼近 1 亿门槛、以及 0
        #expect(TokenFormatter.splitCompact(10_000).unit == "万")
        #expect(TokenFormatter.splitCompact(99_999_999).unit == "万")
        let zero = TokenFormatter.splitCompact(0)
        #expect(zero.number == "0")
        #expect(zero.unit == "")
    }
}

/// 展示短名与时间格式化(口径对齐 frontend/src/utils/format.ts)。
struct RequestLogDisplayTests {
    @Test func protocolShort() {
        #expect(RequestLogDisplay.protocolShort("chat_completions") == "chat")
        #expect(RequestLogDisplay.protocolShort("responses") == "responses")
        #expect(RequestLogDisplay.protocolShort("messages") == "messages")
        #expect(RequestLogDisplay.protocolShort("unknown") == "unknown")
    }

    @Test func modeShort() {
        #expect(RequestLogDisplay.modeShort("native_passthrough") == "透传")
        #expect(RequestLogDisplay.modeShort("converted") == "转换")
        #expect(RequestLogDisplay.modeShort("") == "")
    }

    @Test func agentShortRecognizesTools() {
        #expect(RequestLogDisplay.agentShort("claude-cli/1.2.3") == "Claude Code/1.2.3")
        #expect(RequestLogDisplay.agentShort("claude-code/2.0.0 (darwin arm64)") == "Claude Code/2.0.0")
        #expect(RequestLogDisplay.agentShort("codex/0.9") == "Codex CLI/0.9")
        #expect(RequestLogDisplay.agentShort("gemini-cli/1.0") == "Gemini CLI/1.0")
        #expect(RequestLogDisplay.agentShort("cursor/0.42") == "Cursor/0.42")
        #expect(RequestLogDisplay.agentShort("Dify/1.4.0") == "Dify/1.4.0")
    }

    @Test func agentShortBrowsersAndFallbacks() {
        let chrome = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
            + "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
        #expect(RequestLogDisplay.agentShort(chrome) == "Chrome")
        #expect(RequestLogDisplay.agentShort("Mozilla/5.0 Firefox/121.0") == "Firefox")

        // 认不出的原样保留,不折叠成实现语言
        #expect(RequestLogDisplay.agentShort("python-httpx/0.27.0") == "python-httpx/0.27.0")
        #expect(RequestLogDisplay.agentShort("go-http-client/1.1") == "go-http-client/1.1")
        // 多段产品串不拼版本号
        #expect(RequestLogDisplay.agentShort("OpenAI/NodeJS/5.1") == "OpenAI SDK")
        #expect(RequestLogDisplay.agentShort("") == "—")
        #expect(RequestLogDisplay.agentShort("   ") == "—")
    }

    /// 时间格式为本地时区;固定 GMT 日历让断言稳定。
    @Test func clockFormatting() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try #require(TimeZone(identifier: "GMT"))
        let date = try #require(calendar.date(
            from: DateComponents(year: 2026, month: 9, day: 11, hour: 5, minute: 6, second: 7)
        ))
        #expect(RequestLogDisplay.clock(date, calendar: calendar) == "05:06:07")
        #expect(RequestLogDisplay.fullClock(date, calendar: calendar) == "09-11 05:06:07")
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

/// 数据来源持久化格式(AppSettings 以 JSON 字符串存入 UserDefaults)。
struct DataSourceCodableTests {
    @Test func roundTrips() throws {
        let sources = [
            DataSource(name: "本地", urlString: "http://127.0.0.1:9192"),
            DataSource(name: "远程", urlString: "https://hub.example.com"),
        ]
        let data = try JSONEncoder().encode(sources)
        let list = try JSONDecoder().decode([DataSource].self, from: data)
        #expect(list == sources)
    }
}

/// 渠道 / 余额 / 花费 / 计费设置的真实形状 JSON 解码。
struct ChannelAndCostDecodingTests {
    /// 后端响应含明文 api_key;Channel 不声明该字段,应正常解码且不持有密钥。
    @Test func decodesChannel() throws {
        let json = """
        {"id":3,"name":"DeepSeek","provider":"openai_compatible",
        "base_url":"https://api.deepseek.com/v1","api_key":"sk-secret","models":"deepseek-chat",
        "native_protocols":"chat_completions","priority":100,"weight":1,"status":1,"remark":"",
        "last_probe_at":null,"probe_result":"","created_at":"2026-09-01T00:00:00Z",
        "updated_at":"2026-09-01T00:00:00Z"}
        """
        let ch = try JSONDecoder().decode(Channel.self, from: Data(json.utf8))
        #expect(ch.id == 3)
        #expect(ch.name == "DeepSeek")
        #expect(ch.baseURL == "https://api.deepseek.com/v1")
        #expect(ch.isEnabled)
    }

    @Test func decodesDisabledChannel() throws {
        let json = """
        {"id":9,"name":"备用","provider":"anthropic","base_url":"https://api.anthropic.com",
        "status":0,"priority":200,"remark":"停用"}
        """
        let ch = try JSONDecoder().decode(Channel.self, from: Data(json.utf8))
        #expect(!ch.isEnabled)
        #expect(ch.provider == "anthropic")
    }

    @Test func decodesSupportedBalance() throws {
        let json = """
        {"channel_id":3,"channel_name":"DeepSeek","provider":"deepseek","supported":true,"ok":true,
        "balance":{"provider":"deepseek","is_available":true,"currency":"CNY","total":110.5,
        "granted":10.5,"topped_up":100,"fetched_at":"2026-09-11T10:00:00Z"},
        "fetched_at":"2026-09-11T10:00:00Z"}
        """
        let entry = try JSONDecoder().decode(ChannelBalance.self, from: Data(json.utf8))
        #expect(entry.id == 3)
        #expect(entry.supported)
        #expect(entry.ok)
        let info = try #require(entry.balance)
        #expect(info.currency == "CNY")
        #expect(info.total == 110.5)
        #expect(info.granted == 10.5)
        #expect(info.toppedUp == 100)
        #expect(info.isAvailable)
    }

    /// 不支持的渠道:balance/error 被省略,但仍带零值 fetched_at(Go time.Time 无 omitempty)。
    @Test func decodesUnsupportedBalance() throws {
        let json = """
        {"channel_id":4,"channel_name":"OpenRouter","provider":"","supported":false,"ok":false,
        "fetched_at":"0001-01-01T00:00:00Z"}
        """
        let entry = try JSONDecoder().decode(ChannelBalance.self, from: Data(json.utf8))
        #expect(!entry.supported)
        #expect(!entry.ok)
        #expect(entry.balance == nil)
    }

    @Test func decodesFailedBalance() throws {
        let json = """
        {"channel_id":5,"channel_name":"DeepSeek","provider":"deepseek","supported":true,"ok":false,
        "error":"GET /user/balance → HTTP 401","fetched_at":"2026-09-11T10:00:00Z"}
        """
        let entry = try JSONDecoder().decode(ChannelBalance.self, from: Data(json.utf8))
        #expect(entry.supported)
        #expect(!entry.ok)
        #expect(entry.error == "GET /user/balance → HTTP 401")
        #expect(entry.balance == nil)
    }

    @Test func decodesBalanceList() throws {
        let json = """
        {"items":[{"channel_id":1,"channel_name":"A","provider":"deepseek","supported":true,"ok":false},
        {"channel_id":2,"channel_name":"B","provider":"","supported":false,"ok":false}]}
        """
        let list = try JSONDecoder().decode(ChannelBalanceList.self, from: Data(json.utf8))
        #expect(list.items.count == 2)
        #expect(list.items[0].channelName == "A")
    }

    /// 样本不足:projected_usd 为 null,reason 说明原因。
    @Test func decodesForecastWithoutProjection() throws {
        let json = """
        {"period":"today","period_start":"2026-09-11T00:00:00+08:00",
        "period_end":"2026-09-12T00:00:00+08:00","spent_usd":0,"projected_usd":null,
        "burn_per_hour_usd":0,"daily_avg_usd":0,"basis":"linear","confidence":"low",
        "reason":"本周期暂无花费","budget_usd":0}
        """
        let f = try JSONDecoder().decode(CostForecast.self, from: Data(json.utf8))
        #expect(f.period == "today")
        #expect(f.projectedUSD == nil)
        #expect(f.reason == "本周期暂无花费")
        #expect(f.spentUSD == 0)
    }

    @Test func decodesForecastWithBudget() throws {
        let json = """
        {"period":"month","spent_usd":12.5,"projected_usd":48.75,"burn_per_hour_usd":0.1,
        "daily_avg_usd":1.6,"basis":"run_rate","confidence":"high","budget_usd":50,
        "projected_exceeded_date":"2026-09-28 14:30"}
        """
        let f = try JSONDecoder().decode(CostForecast.self, from: Data(json.utf8))
        #expect(f.projectedUSD == 48.75)
        #expect(f.basis == "run_rate")
        #expect(f.budgetUSD == 50)
        #expect(f.projectedExceededDate == "2026-09-28 14:30")
    }

    @Test func decodesBillingSettings() throws {
        let json = """
        {"display_currency":"CNY","usd_cny_rate":7.2,"monthly_budget_usd":50}
        """
        let b = try JSONDecoder().decode(BillingSettings.self, from: Data(json.utf8))
        #expect(b.displayCurrency == "CNY")
        #expect(b.usdRate == 7.2)
        #expect(b.monthlyBudgetUSD == 50)
    }

    @Test func decodesBalanceUpdatedEvent() throws {
        let text = """
        {"type":"channel.balance_updated","ts":1,"payload":{"channel_id":3,"channel_name":"DeepSeek",
        "balance":{"provider":"deepseek","is_available":true,"currency":"CNY","total":110.5,
        "granted":10.5,"topped_up":100,"fetched_at":"2026-09-11T10:00:00Z"}}}
        """
        guard case .channelBalanceUpdated(let p) = WsDecoder.decode(text) else {
            Issue.record("应为 channelBalanceUpdated 事件")
            return
        }
        #expect(p.channelID == 3)
        #expect(p.channelName == "DeepSeek")
        #expect(p.balance.total == 110.5)
    }

    /// 载荷畸形时降级为 channelUpdated,仍能触发重拉。
    @Test func malformedBalanceUpdatedFallsBack() {
        #expect(WsDecoder.decode(#"{"type":"channel.balance_updated","payload":{"channel_id":"x"},"ts":1}"#) == .channelUpdated)
        #expect(WsDecoder.decode(#"{"type":"channel.balance_updated","ts":1}"#) == .channelUpdated)
    }
}

/// 金额格式化(与前端 money.ts 同口径)。
struct MoneyFormatterTests {
    @Test func formatsZeroAndInvalid() {
        #expect(MoneyFormatter.format(usd: 0) == "$0")
        #expect(MoneyFormatter.format(usd: .nan) == "$—")
        #expect(MoneyFormatter.format(usd: .infinity) == "$—")
    }

    @Test func digitTiersAndGrouping() {
        #expect(MoneyFormatter.digits(for: 0.5) == 4)
        #expect(MoneyFormatter.digits(for: 12.5) == 3)
        #expect(MoneyFormatter.digits(for: 1234.5) == 2)
        #expect(MoneyFormatter.format(usd: 1.25) == "$1.250")
        #expect(MoneyFormatter.format(usd: 12.5) == "$12.500")
        #expect(MoneyFormatter.format(usd: 1234.5) == "$1,234.50")
    }

    @Test func convertsToCNY() {
        let cny = MoneyFormatter.Options(currency: "CNY", rate: 7.2)
        #expect(MoneyFormatter.format(usd: 1, options: cny) == "¥7.200")
    }

    /// 选了 CNY 但汇率未配置时回退 USD,避免把美元数字套上 ¥。
    @Test func cnyWithoutRateFallsBackToUSD() {
        let cnyNoRate = MoneyFormatter.Options(currency: "CNY", rate: 0)
        #expect(MoneyFormatter.format(usd: 1, options: cnyNoRate) == "$1.000")
        #expect(cnyNoRate.normalized == .usd)
    }

    @Test func balanceSymbols() {
        #expect(MoneyFormatter.balanceSymbol(for: "CNY") == "¥")
        #expect(MoneyFormatter.balanceSymbol(for: "USD") == "$")
        #expect(MoneyFormatter.balanceSymbol(for: "EUR") == "EUR ")
        #expect(MoneyFormatter.balanceSymbol(for: nil) == "$")
        #expect(MoneyFormatter.balanceSymbol(for: "") == "$")
        #expect(MoneyFormatter.formatBalance(110.5, currency: "CNY") == "¥110.50")
    }
}
