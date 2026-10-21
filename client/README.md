# IM2Agent 客户端（v0.8-client）

Flutter 单代码库，三端同跑：**iOS / Android / Windows**（macOS/web 也兼容）。

## 前置：装 Flutter SDK

```bash
# macOS 推荐
brew install --cask flutter
# 或 https://docs.flutter.dev/get-started/install
flutter doctor   # 三端工具链都要打勾
```

## 1. 起后端（demo）

```bash
# 在仓库根目录
cd /path/to/im2agent
export OPENAI_API_KEY=sk-xxx   # 可选，不配则模板降级
go run ./cmd/demo -addr :8080
```

## 2. 跑客户端

```bash
cd client/app
flutter pub get

# Windows 桌面（Windows 上）
flutter run -d windows

# Android 真机/模拟器（手机开 USB 调试，和电脑同一 WiFi）
flutter run -d <android-device-id>

# iOS 真机（macOS + Xcode）
flutter run -d <ios-device-id>
```

**手机连后端的地址**：编辑 `lib/services/api_client.dart` 里的 `baseUrl`，
改成电脑局域网 IP（例如 `http://192.168.1.100:8080`）。手机不能用 `127.0.0.1`。

> Android 9+ 默认禁明文 HTTP，调试时在 `android/app/src/main/AndroidManifest.xml` 加：
> ```xml
> <application android:usesCleartextTraffic="true" ...>
> ```
> iOS 在 `Info.plist` 加 `NSAppTransportSecurity -> NSAllowsArbitraryLoads = true`（仅调试）。

## 3. 打包三端发布版

```bash
# Windows exe
flutter build windows
# 产物 build/windows/x64/runner/Release/

# Android APK
flutter build apk --release
# 产物 build/app/outputs/flutter-apk/app-release.apk

# iOS IPA（需 Apple 开发者账号）
flutter build ipa --release
```

## 三端能力对照

| 能力 | iOS | Android | Windows |
| --- | --- | --- | --- |
| 审批态（通过/驳回/修改） | ✅ | ✅ | ✅ |
| 巡视态（Agent 团队） | ✅ | ✅ | ✅ |
| 会话态 | ✅ | ✅ | ✅ |
| 后台推送（FCM/APNs） | 待接 | 待接 | — |
| 手表端精简 UI | 待接（WatchOS） | 待接（Wear OS） | — |

## 代码结构

```
client/app/lib/
├── main.dart                  # 三态导航（审批/团队/会话）
├── services/api_client.dart   # 真连后端 /api/admin/state + /api/approval/decide + /api/llm/draft
├── models/                    # ApprovalTicket / AgentStatus / StrangerIntent
├── screens/
│   ├── approval_screen.dart    # 审批态（默认）
│   ├── overview_screen.dart    # 巡视态
│   └── conversation_screen.dart # 会话态
└── widgets/approval_card.dart # 审批卡（通过/修改/驳回）
```

## 已知边界

- 本机未装 Flutter SDK，Dart 代码未实机编译；`flutter pub get && flutter run` 即可验证
- 无登录/鉴权：所有请求假设后端在可信网络（局域网或本机）
- 状态靠下拉刷新，无 WebSocket 长连（v0.9 生产版接腾讯云 IM WebSocket）
