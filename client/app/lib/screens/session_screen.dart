/// A2A 协商会话屏（v0.1.8）：手机端看 Agent 之间的多轮对话。
library;

import 'dart:async';

import 'package:flutter/material.dart';

import '../services/api_client.dart';

class SessionScreen extends StatefulWidget {
  const SessionScreen({super.key, this.client});

  final ApiClient? client;

  @override
  State<SessionScreen> createState() => _SessionScreenState();
}

class _SessionScreenState extends State<SessionScreen> {
  late final ApiClient _client = widget.client ?? ApiClient();
  List<Map<String, dynamic>> _sessions = [];
  StreamSubscription? _sseSub;

  @override
  void initState() {
    super.initState();
    _refresh();
    _sseSub = _client.streamEvents().listen((_) => _refresh());
  }

  @override
  void dispose() {
    _sseSub?.cancel();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final s = await _client.fetchSessions();
      if (mounted) setState(() => _sessions = s);
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Agent 协商'), actions: [
        IconButton(onPressed: _refresh, icon: const Icon(Icons.refresh)),
      ]),
      body: _sessions.isEmpty
          ? const Center(child: Text('暂无协商会话', style: TextStyle(color: Colors.grey)))
          : ListView(
              padding: const EdgeInsets.all(12),
              children: _sessions.map((s) => Card(
                    child: ExpansionTile(
                      leading: const Icon(Icons.hub_outlined),
                      title: Text('${s['id']} · ${s['state']}'),
                      subtitle: Text('轮次 ${s['round']}/${s['max_rounds']}'
                          '${s['ticket_id'] != null ? ' · 审批卡 ${s['ticket_id']}' : ''}'),
                      children: ((s['turns'] as List<dynamic>? ?? [])).map((t) {
                        final m = t as Map<String, dynamic>;
                        return ListTile(
                          dense: true,
                          leading: const Icon(Icons.arrow_forward, size: 16),
                          title: Text('${m['from']} → ${m['to']}  [${m['intent']}]'),
                          subtitle: Text('${m['payload']}'),
                        );
                      }).toList(),
                    ),
                  )).toList(),
            ),
    );
  }
}
