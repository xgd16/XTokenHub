import AppKit

// 生成 1024x1024 AppIcon:macOS 圆角方形(栅格留边),蓝青渐变玻璃底 +
// 顶部高光、图表网格、带阴影的上升折线与端点圆点。
// 用法: swift Tools/make_icon.swift(在 XTokenHubMenuBar/ 目录下执行)

let size = 1024
guard let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil,
    pixelsWide: size,
    pixelsHigh: size,
    bitsPerSample: 8,
    samplesPerPixel: 4,
    hasAlpha: true,
    isPlanar: false,
    colorSpaceName: .deviceRGB,
    bytesPerRow: 0,
    bitsPerPixel: 0
) else { fatalError("创建位图失败") }
rep.size = NSSize(width: size, height: size)

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)

let s = CGFloat(size)

// macOS 图标栅格:四周留边约 9.77%,圆角为边长的 22.5%
let inset = s * 0.0977
let rect = NSRect(x: inset, y: inset, width: s - inset * 2, height: s - inset * 2)
let radius = rect.width * 0.225
let squircle = NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius)

// 1. 背景渐变:品牌蓝 → 青(与 AccentColor 一致)
NSGradient(colors: [
    NSColor(srgbRed: 0.043, green: 0.451, blue: 0.851, alpha: 1),
    NSColor(srgbRed: 0.118, green: 0.784, blue: 0.878, alpha: 1),
])!.draw(in: squircle, angle: -70)

// 后续内容裁剪在圆角内
NSGraphicsContext.saveGraphicsState()
squircle.addClip()

// 2. 玻璃高光:自下而上淡入,顶部提亮
NSGradient(colors: [
    NSColor(white: 1, alpha: 0.04),
    NSColor(white: 1, alpha: 0.30),
])!.draw(in: rect, angle: 90)

// 3. 图表网格线
NSColor(white: 1, alpha: 0.10).setFill()
for f in [0.24, 0.44, 0.64] {
    let y = rect.minY + rect.height * f
    NSBezierPath(rect: NSRect(x: rect.minX + rect.width * 0.13, y: y, width: rect.width * 0.74, height: s * 0.0035)).fill()
}

// 4. 折线数据点(相对 rect 比例,y 自底向上):整体向上,收于右上
let ratios: [(CGFloat, CGFloat)] = [(0.15, 0.28), (0.33, 0.46), (0.48, 0.36), (0.65, 0.52), (0.85, 0.72)]
let pts = ratios.map { NSPoint(x: rect.minX + rect.width * $0.0, y: rect.minY + rect.height * $0.1) }

// 5. 上升折线(带投影)
NSGraphicsContext.saveGraphicsState()
let shadow = NSShadow()
shadow.shadowColor = NSColor(white: 0, alpha: 0.35)
shadow.shadowOffset = NSSize(width: 0, height: -s * 0.012)
shadow.shadowBlurRadius = s * 0.022
shadow.set()
let line = NSBezierPath()
line.lineWidth = s * 0.055
line.lineCapStyle = .round
line.lineJoinStyle = .round
line.move(to: pts[0])
pts.dropFirst().forEach { line.line(to: $0) }
NSColor.white.setStroke()
line.stroke()
NSGraphicsContext.restoreGraphicsState()

// 6. 两端圆点:外圈半透明光晕 + 白色实心
for p in [pts[0], pts[4]] {
    NSColor(white: 1, alpha: 0.35).setFill()
    NSBezierPath(ovalIn: NSRect(x: p.x - s * 0.072, y: p.y - s * 0.072, width: s * 0.144, height: s * 0.144)).fill()
    NSColor.white.setFill()
    NSBezierPath(ovalIn: NSRect(x: p.x - s * 0.042, y: p.y - s * 0.042, width: s * 0.084, height: s * 0.084)).fill()
}

NSGraphicsContext.restoreGraphicsState()  // 解除圆角裁剪
NSGraphicsContext.restoreGraphicsState()  // 恢复图形上下文

guard let png = rep.representation(using: .png, properties: [:]) else { fatalError("PNG 编码失败") }
let iconDir = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
    .appendingPathComponent("App/Assets.xcassets/AppIcon.appiconset")
try png.write(to: iconDir.appendingPathComponent("AppIcon.png"))

// 派生 macOS 多尺寸图标(16~1024,actool 传统 mac 图标集)
let sizes: [(name: String, px: Int)] = [
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024),
]
for spec in sizes {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/usr/bin/sips")
    p.arguments = ["-z", "\(spec.px)", "\(spec.px)", iconDir.appendingPathComponent("AppIcon.png").path,
                   "--out", iconDir.appendingPathComponent(spec.name).path]
    try p.run()
    p.waitUntilExit()
}
print("已生成 1024 master + \(sizes.count) 个尺寸 → \(iconDir.path)")
