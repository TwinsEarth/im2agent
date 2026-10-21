/// 审计流屏（v0.1.6）：只读、不可变事件时间线。
library;

import 'package:flutter/material.dart';

import '../services/api_client.dart';

class AuditScreen extends StatefulWidget {
  const AuditScreen({super.key, this.client});

  final ApiClient? client;

  @override
  State<AuditScreen> createState() => _AuditScreenState();
}

class _AuditScreenState extends State<AuditScreen> {
  late final ApiClient _client = widget.client ?? ApiClient();
  List<Map<String, dynamic>> _events = [];

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final ev = await _client.fetchAudit();
      if (mounted) setState(() => _events = ev.reversed.toList());
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('审计流'), actions: [
        IconButton(onPressed: _refresh, icon: const Icon(Icons.refresh)),
      ]),
      body: _events.isEmpty
          ? const Center(child: Text('暂无审计事件', style: TextStyle(color: Colors.grey)))
          : ListView.separated(
              itemCount: _events.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (_, i) {
                final e = _events[i];
                return ListTile(
                  leading: const Icon(Icons.lock_outline, size: 20),
                  title: Text('${e['action'] ?? '-'} · ${e['agent_id'] ?? '-'}'),
                  subtitle: Text('${e['decision'] ?? ''}\n${e['detail'] ?? ''}'),
                  isThreeLine: true,
                );
              },
            ),
    );
  }
}
