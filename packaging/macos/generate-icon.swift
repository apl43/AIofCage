import AppKit
import WebKit

// SVG 是唯一设计源；使用系统 WebKit 渲染，并生成标准 macOS 多尺寸图标。
final class IconRenderer: NSObject, WKNavigationDelegate {
    private let webView: WKWebView
    private let outputDirectory: URL
    private let svg: String

    init(source: URL, outputDirectory: URL) throws {
        self.outputDirectory = outputDirectory
        svg = try String(contentsOf: source, encoding: .utf8)
        webView = WKWebView(frame: NSRect(x: 0, y: 0, width: 1024, height: 1024))
        super.init()
        webView.navigationDelegate = self
        webView.setValue(false, forKey: "drawsBackground")
    }

    func render() {
        webView.loadHTMLString("""
        <!doctype html><html><head><meta name="viewport" content="width=1024, initial-scale=1">
        <style>html,body{margin:0;width:1024px;height:1024px;background:transparent}svg{display:block}</style>
        </head><body>\(svg)</body></html>
        """, baseURL: nil)
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        let configuration = WKSnapshotConfiguration()
        configuration.rect = NSRect(x: 0, y: 0, width: 1024, height: 1024)
        configuration.snapshotWidth = 1024
        webView.takeSnapshot(with: configuration) { image, error in
            do {
                if let error { throw error }
                guard let image else { throw CocoaError(.fileReadUnknown) }
                try self.export(image)
                print("已生成 AppIcon.png 和 AppIcon.icns")
                exit(EXIT_SUCCESS)
            } catch {
                self.fail(error)
            }
        }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        fail(error)
    }

    private func export(_ image: NSImage) throws {
        let files = FileManager.default
        try files.createDirectory(at: outputDirectory, withIntermediateDirectories: true)
        let iconset = files.temporaryDirectory.appendingPathComponent("AIofCage-\(UUID().uuidString).iconset")
        try files.createDirectory(at: iconset, withIntermediateDirectories: true)
        defer { try? files.removeItem(at: iconset) }

        for points in [16, 32, 128, 256, 512] {
            for scale in [1, 2] {
                let pixels = points * scale
                guard let bitmap = NSBitmapImageRep(
                    bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
                    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
                ), let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
                    throw CocoaError(.fileWriteUnknown)
                }
                NSGraphicsContext.saveGraphicsState()
                NSGraphicsContext.current = context
                context.imageInterpolation = .high
                image.draw(in: NSRect(x: 0, y: 0, width: pixels, height: pixels))
                NSGraphicsContext.restoreGraphicsState()
                guard let png = bitmap.representation(using: .png, properties: [:]) else {
                    throw CocoaError(.fileWriteUnknown)
                }
                let suffix = scale == 2 ? "@2x" : ""
                try png.write(to: iconset.appendingPathComponent("icon_\(points)x\(points)\(suffix).png"))
                if pixels == 1024 {
                    try png.write(to: outputDirectory.appendingPathComponent("AppIcon.png"))
                }
            }
        }

        let iconutil = Process()
        iconutil.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
        iconutil.arguments = ["-c", "icns", iconset.path, "-o", outputDirectory.appendingPathComponent("AppIcon.icns").path]
        try iconutil.run()
        iconutil.waitUntilExit()
        guard iconutil.terminationStatus == 0 else { throw CocoaError(.fileWriteUnknown) }
    }

    private func fail(_ error: Error) -> Never {
        FileHandle.standardError.write(Data("图标生成失败：\(error.localizedDescription)\n".utf8))
        exit(EXIT_FAILURE)
    }
}

guard CommandLine.arguments.count == 3 else {
    FileHandle.standardError.write(Data("用法：swift generate-icon.swift <源 SVG> <输出目录>\n".utf8))
    exit(EXIT_FAILURE)
}
let application = NSApplication.shared
application.setActivationPolicy(.prohibited)
let renderer = try IconRenderer(
    source: URL(fileURLWithPath: CommandLine.arguments[1]),
    outputDirectory: URL(fileURLWithPath: CommandLine.arguments[2])
)
// 渲染服务异常时给出确定的失败结果，避免构建一直等待。
DispatchQueue.main.asyncAfter(deadline: .now() + 30) {
    FileHandle.standardError.write(Data("图标生成超时\n".utf8))
    exit(EXIT_FAILURE)
}
renderer.render()
application.run()
