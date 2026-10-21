/// API 客户端（v0.8 真连后端）。
///
/// 后端是 cmd/demo 起的单二进制服务，默认 http://127.0.0.1:8080。
/// 真机调试时改为电脑局域网 IP（手机和电脑同一 WiFi）。
library;

import 'dart:convert';
import 'package:http/http.dart' as http;

class ApiClient {
  ApiClient({this.baseUrl = 'http://127.0.0.1:8080'});

  String baseUrl;

  Uri _u(String path) => Uri.parse('$baseUrl$path');

  /// 拉取本人待审列表（demo 聚合端点）。
  Future<List<Map<String, dynamic>>> fetchPendingApprovals() async {
    final r = await http.get(_u('/api/admin/state'));
    if (r.statusCode != 200) throw Exception('state ${r.statusCode}');
    final data = jsonDecode(r.body) as Map<String, dynamic>;
    return (data['pending'] as List<dynamic>)
        .map((e) => e as Map<String, dynamic>)
        .toList();
  }

  /// 对审批单执行动作：approve / reject / modify。
  ///
  /// 与后端 v0.0.1 对齐：POST /api/approval/decide，body 为 snake_case。
  Future<Map<String, dynamic>> submitApproval({
    required String ticketId,
    required String action,
    String? note,
  }) async {
    final r = await http.post(
      _u('/api/approval/decide'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'ticket_id': ticketId,
        'decision': action,
        'note': note ?? '',
      }),
    );
    if (r.statusCode != 200) throw Exception('decide ${r.statusCode}: ${r.body}');
    return jsonDecode(r.body) as Map<String, dynamic>;
  }

  /// 拉取付费账本流水（demo 聚合端点）。
  Future<Map<String, dynamic>> fetchLedger() async {
    final r = await http.get(_u('/api/admin/ledger'));
    if (r.statusCode != 200) throw Exception('ledger ${r.statusCode}');
    return jsonDecode(r.body) as Map<String, dynamic>;
  }

  /// v0.2.1：拉取 Context 飞轮指标。
  Future<Map<String, dynamic>> fetchMetrics() async {
    final r = await http.get(_u('/api/admin/state'));
    if (r.statusCode != 200) return {};
    final data = jsonDecode(r.body) as Map<String, dynamic>;
    return (data['metrics'] as Map<String, dynamic>?) ?? {};
  }

  /// v0.1.8：拉取 A2A 协商会话列表（含每轮 turns）。
  Future<List<Map<String, dynamic>>> fetchSessions() async {
    final r = await http.get(_u('/api/admin/state'));
    if (r.statusCode != 200) return [];
    final data = jsonDecode(r.body) as Map<String, dynamic>;
    return (data['sessions'] as List<dynamic>? ?? [])
        .map((e) => e as Map<String, dynamic>)
        .toList();
  }

  /// v0.1.6：拉取不可变审计事件流。
  Future<List<Map<String, dynamic>>> fetchAudit() async {
    final r = await http.get(_u('/api/admin/audit'));
    if (r.statusCode != 200) return [];
    final data = jsonDecode(r.body) as Map<String, dynamic>;
    return (data['events'] as List<dynamic>? ?? [])
        .map((e) => e as Map<String, dynamic>)
        .toList();
  }

  /// v0.1.5：拉取本人当前全部信任授权列表。
  Future<List<Map<String, dynamic>>> fetchTrusts() async {
    final r = await http.get(_u('/api/admin/state'));
    if (r.statusCode != 200) throw Exception('state ${r.statusCode}');
    final data = jsonDecode(r.body) as Map<String, dynamic>;
    return (data['trusts'] as List<dynamic>? ?? [])
        .map((e) => e as Map<String, dynamic>)
        .toList();
  }

  /// v0.1.5：给指定 targetId 授予 0..4 档信任。
  Future<void> grantTrust({required int targetId, required int level}) async {
    final r = await http.post(
      _u('/api/relation/v1/trust/grant'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'userId': 100, 'targetId': targetId, 'level': level}),
    );
    if (r.statusCode != 200) throw Exception('grant ${r.statusCode}: ${r.body}');
  }

  /// 探测后端是否配了 LLM。
  Future<Map<String, dynamic>> llmStatus() async {    try {
      final r = await http.get(_u('/api/llm/status'));
      if (r.statusCode != 200) return {'ready': false};
      return jsonDecode(r.body) as Map<String, dynamic>;
    } catch (_) {
      return {'ready': false};
    }
  }

  /// 让 Agent 替本人起草一段陌生人回复。
  Future<Map<String, dynamic>> llmDraft({
    required String category,
    required String content,
    String tone = 'polite',
  }) async {
    final r = await http.post(
      _u('/api/llm/draft'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'category': category,
        'content': content,
        'tone': tone,
      }),
    );
    if (r.statusCode != 200) throw Exception('draft ${r.statusCode}');
    return jsonDecode(r.body) as Map<String, dynamic>;
  }

  /// 健康检查。
  Future<bool> healthz() async {
    try {
      final r = await http.get(_u('/healthz'));
      return r.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// v0.1.4：订阅后端 SSE 事件流（/api/stream）。
  ///
  /// 后端用纯 stdlib 实现的 text/event-stream；每条 `data:` 行 JSON 解码后产出。
  /// 连接断开时返回的 Stream 会自动关闭，调用方负责重连。
  Stream<Map<String, dynamic>> streamEvents() async* {
    final client = http.Client();
    try {
      final req = http.Request('GET', _u('/api/stream'));
      final res = await client.send(req);
      if (res.statusCode != 200) return;
      final buf = StringBuffer();
      await for (final chunk in res.stream.transform(utf8.decoder)) {
        for (final ch in chunk.split('')) {
          if (ch == '\n') {
            final line = buf.toString();
            buf.clear();
            if (line.startsWith('data:')) {
              final payload = line.substring(5).trim();
              if (payload.isEmpty) continue;
              try {
                final decoded = jsonDecode(payload);
                if (decoded is Map<String, dynamic>) yield decoded;
              } catch (_) {/* 心跳或非 JSON 行忽略 */}
            }
          } else {
            buf.write(ch);
          }
        }
      }
    } finally {
      client.close();
    }
  }
}
