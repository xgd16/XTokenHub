import AppKit
import SwiftUI
import Testing
@testable import XTokenHubMenuBar

/// 菜单栏/浮窗共用的指标格式化、可见性守卫与浮窗几何。
@MainActor
struct SurfaceMetricsTests {
    private var allMetrics: Set<AppSettings.MenuBarMetric> {
        Set(AppSettings.MenuBarMetric.allCases)
    }

    private func fullSnapshot() -> SurfaceMetricSnapshot {
        SurfaceMetricSnapshot(
            totalRequests: 482,
            todayTokens: 32_144_500,
            cacheHitRate: 0.96,
            costTodayUSD: 1.234,
            tokensPerSec: 12,
            peak: .peak,
            displayCurrency: "USD",
            usdRate: 0,
            isOnline: true,
            sourceName: "本地",
            activeStreams: 2
        )
    }

    @Test func partsFollowFixedOrder() {
        let parts = SurfaceMetrics.parts(fullSnapshot(), metrics: allMetrics)
        #expect(parts == ["482", "3214.45万", "96.0%", "$1.234", "12/s", "峰"])
    }

    @Test func partsKeepPlaceholdersWhenDataMissing() {
        let snapshot = SurfaceMetricSnapshot()
        let parts = SurfaceMetrics.parts(
            snapshot,
            metrics: [.peakStatus, .costToday, .cacheHit, .tokens, .requests]
        )
        // 请求数缺省沿袭原菜单栏行为显示 0;其余未知显示 —;没有时段价模型时峰谷整段省略。
        #expect(parts == ["0", "0", "—", "—"])
    }

    @Test func itemsFollowFixedOrderAndSplitTokenUnit() {
        let items = SurfaceMetrics.items(fullSnapshot(), metrics: allMetrics)
        #expect(items.map(\.title) == ["请求 · 今日", "Token · 今日", "缓存命中", "今日花费", "实时速度", "峰谷"])
        #expect(items.map(\.value) == ["482", "3214.45", "96.0%", "$1.234", "12", "峰"])
        #expect(items.map(\.unit) == ["", "万", "", "", "/s", ""])
    }

    @Test func itemsShowDashForMissingPeak() {
        var snapshot = fullSnapshot()
        snapshot.peak = nil
        let item = SurfaceMetrics.items(snapshot, metrics: [.peakStatus]).first
        #expect(item?.value == "—")
        #expect(item?.tint == Color.secondary)
    }

    @Test func itemsConvertCostToDisplayCurrency() {
        var snapshot = fullSnapshot()
        snapshot.displayCurrency = "CNY"
        snapshot.usdRate = 7.2
        let item = SurfaceMetrics.items(snapshot, metrics: [.costToday]).first
        #expect(item?.value == "¥8.885")
    }

    // MARK: - 可见性守卫

    @Test func visibilityNeverLeavesNoEntry() {
        let bothOff = AppSettings.resolveVisibility(menuBar: false, floating: false)
        #expect(bothOff.menuBar)
        #expect(!bothOff.floating)

        let menuBarOnly = AppSettings.resolveVisibility(menuBar: true, floating: false)
        #expect(menuBarOnly.menuBar)
        #expect(!menuBarOnly.floating)

        let floatingOnly = AppSettings.resolveVisibility(menuBar: false, floating: true)
        #expect(!floatingOnly.menuBar)
        #expect(floatingOnly.floating)
    }

    // MARK: - 浮窗几何

    @Test func defaultOriginSitsBelowMenuBar() {
        let visible = NSRect(x: 0, y: 0, width: 1512, height: 940)
        let size = NSSize(width: 264, height: 200)
        let origin = FloatingPlacement.defaultOrigin(visibleFrame: visible, size: size)
        #expect(origin.x == visible.maxX - size.width - 12)
        #expect(origin.y == visible.maxY - size.height - 12)
        #expect(origin.y + size.height <= visible.maxY)
    }

    @Test func resizePreservesTopLeft() {
        let frame = NSRect(x: 100, y: 500, width: 264, height: 200)
        let resized = FloatingPlacement.resizedFramePreservingTopLeft(
            frame,
            size: NSSize(width: 264, height: 320)
        )
        #expect(resized.origin.x == 100)
        #expect(resized.maxY == frame.maxY)
        #expect(resized.height == 320)
    }

    @Test func clampKeepsPanelOnScreen() {
        let screens = [
            NSRect(x: 0, y: 0, width: 1512, height: 940),
            NSRect(x: 1512, y: 0, width: 1920, height: 1080),
        ]
        // 完全不与任何屏幕相交(例如拔掉外接屏后):回退到主屏。
        let offscreen = NSRect(x: 4000, y: 3000, width: 264, height: 200)
        let clamped = FloatingPlacement.clampedFrame(offscreen, visibleFrames: screens)
        #expect(clamped.maxX <= screens[0].maxX)
        #expect(clamped.maxY <= screens[0].maxY)
        #expect(clamped.origin.x >= screens[0].minX)
        #expect(clamped.origin.y >= screens[0].minY)

        // 与外接屏有交集时留在外接屏内。
        let onSecond = NSRect(x: 1600, y: 100, width: 264, height: 200)
        let kept = FloatingPlacement.clampedFrame(onSecond, visibleFrames: screens)
        #expect(kept == onSecond)
    }

    @Test func dashboardPrefersLeftOfCard() {
        let visible = NSRect(x: 0, y: 0, width: 1512, height: 940)
        let card = NSRect(x: 1236, y: 700, width: 264, height: 200)
        let size = NSSize(width: 400, height: 760)
        let origin = FloatingPlacement.dashboardOrigin(
            cardFrame: card,
            size: size,
            visibleFrame: visible
        )
        #expect(origin.x == card.minX - size.width - 8)
        #expect(origin.y == card.maxY - size.height)
        #expect(origin.x + size.width <= card.minX)
    }

    @Test func dashboardFallsBackBelowWhenLeftIsCramped() {
        let visible = NSRect(x: 0, y: 0, width: 1512, height: 940)
        let card = NSRect(x: 100, y: 600, width: 264, height: 200)
        let size = NSSize(width: 400, height: 300)
        let origin = FloatingPlacement.dashboardOrigin(
            cardFrame: card,
            size: size,
            visibleFrame: visible
        )
        #expect(origin.x == card.maxX - size.width)
        #expect(origin.y == card.minY - size.height - 8)
    }

    // MARK: - 离屏排版

    private func fittingSize(_ view: some View) -> CGSize {
        let host = NSHostingView(rootView: view)
        host.setFrameSize(
            NSSize(
                width: FloatingCardContent.contentWidth + FloatingCardContent.outerPadding * 2,
                height: 0
            )
        )
        host.layoutSubtreeIfNeeded()
        return host.fittingSize
    }

    private func card(items: [StatItem]) -> FloatingCardContent {
        FloatingCardContent(
            snapshot: fullSnapshot(),
            items: items,
            style: .card,
            onToggleDashboard: {},
            onOpenDashboard: {},
            onToggleStyle: {},
            onOpenSettings: {},
            onResetPosition: {},
            onHide: {}
        )
    }

    private func barView(parts: [String]) -> FloatingBarContent {
        FloatingBarContent(
            snapshot: fullSnapshot(),
            parts: parts,
            style: .bar,
            onToggleDashboard: {},
            onOpenDashboard: {},
            onToggleStyle: {},
            onOpenSettings: {},
            onResetPosition: {},
            onHide: {}
        )
    }

    private func barFittingSize(parts: [String]) -> CGSize {
        let host = NSHostingView(rootView: barView(parts: parts))
        // 模拟真实切换路径:宿主当前还是卡片的窄宽度,再按控制器的探测方式量 ideal size。
        // 直接 setFrameSize(800) 会掩盖「窄窗口把单行文本压成截断宽度」的回归。
        host.setFrameSize(NSSize(width: FloatingCardContent.contentWidth + FloatingCardContent.outerPadding * 2, height: 200))
        host.layoutSubtreeIfNeeded()
        return FloatingSizing.fittingSize(of: host)
    }

    @Test func cardWidthIsFixedAndHeightGrowsWithMetrics() {
        let six = fittingSize(card(items: SurfaceMetrics.items(fullSnapshot(), metrics: allMetrics)))
        let one = fittingSize(card(items: SurfaceMetrics.items(fullSnapshot(), metrics: [.tokens])))
        let empty = fittingSize(card(items: []))

        #expect(six.width == FloatingCardContent.contentWidth + FloatingCardContent.outerPadding * 2)
        #expect(six.height > 0)
        #expect(six.height < 320, "6 个指标时卡片高度 \(six.height)pt 超出预算")
        #expect(one.height < six.height)
        #expect(empty.height < one.height)
    }

    @Test func barStaysSingleLineAndGrowsWithMetrics() {
        let six = barFittingSize(parts: SurfaceMetrics.parts(fullSnapshot(), metrics: allMetrics))
        let one = barFittingSize(parts: SurfaceMetrics.parts(fullSnapshot(), metrics: [.tokens]))
        let empty = barFittingSize(parts: [])

        #expect(six.height > 0)
        #expect(six.height < 44, "6 个指标时条状高度 \(six.height)pt 超出预算")
        #expect(six.width > one.width)
        #expect(six.width > empty.width)
        #expect(six.width < 520, "6 个指标时条状宽度 \(six.width)pt 超出预算")
    }

    /// 条状文本必须给出 intrinsic 宽度,不能因为宿主还停在卡片的窄宽度就自我压缩。
    @Test func barKeepsFullWidthWhenHostStartsNarrow() {
        let host = NSHostingView(rootView: barView(parts: SurfaceMetrics.parts(fullSnapshot(), metrics: allMetrics)))
        host.setFrameSize(
            NSSize(width: FloatingCardContent.contentWidth + FloatingCardContent.outerPadding * 2, height: 200)
        )
        host.layoutSubtreeIfNeeded()
        #expect(host.fittingSize.width > 300, "窄宿主下条状宽度只有 \(host.fittingSize.width)pt")
    }

    /// 反向回归:宿主还是旧卡片的窄宽度时反复量,宽度不能收敛到截断值(越量越短)。
    @Test func barWidthIsStableAcrossRepeatedMeasurements() {
        let host = NSHostingView(rootView: barView(parts: SurfaceMetrics.parts(fullSnapshot(), metrics: allMetrics)))
        host.setFrameSize(
            NSSize(width: FloatingCardContent.contentWidth + FloatingCardContent.outerPadding * 2, height: 200)
        )
        host.layoutSubtreeIfNeeded()

        var widths: [CGFloat] = []
        for _ in 0..<3 {
            let size = FloatingSizing.fittingSize(of: host)
            widths.append(size.width)
            // 模拟控制器把窗口缩到这次的测量值,下一次再从新宽度量。
            host.setFrameSize(size)
            host.layoutSubtreeIfNeeded()
        }

        #expect(widths[0] > 300, "6 个指标条状宽度只有 \(widths[0])pt,最后一个指标会被省略")
        #expect(Set(widths).count == 1, "重复测量宽度漂移: \(widths)")
    }
}
