/// Context 飞轮屏（v0.2.1）：手机端看个人 Agent 的关键指标。
library;

import 'dart:async';

import 'package:flutter/material.dart';

import '../services/api_client.dart';

class MetricScreen extends StatefulWidget {
  const MetricScreen({super.key, this.client});

  final ApiClient? client;

  @override
  State<MetricScreen> createState() => _MetricScreenState();
}

class _MetricScreenState extends State<MetricScreen> {
  late final ApiClient _client = widget.client ?? ApiClient();
  Map<String, dynamic> _m = {};
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
      final m = await _client.fetchMetrics();
      if (mounted) setState(() => _m = m);
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    final items = [
      ('已批准', _m['approval_approved']),
      ('已驳回', _m['approval_rejected']),
      ('已修改', _m['approval_modified']),
      ('陌生人扣费（分）', _m['stranger_charged_cents']),
      ('审计事件总数', _m['audit_events_total']),
    ];
    return Scaffold(
      appBar: AppBar(title: const Text('Context 飞轮'), actions: [
        IconButton(onPressed: _refresh, icon: const Icon(Icons.refresh)),
      ]),
      body: GridView.count(
        padding: const EdgeInsets.all(16),
        crossAxisCount: 2,
        childAspectRatio: 1.4,
        children: items.map((e) => Card(
              child: Center(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text('${e.$2 ?? '-'}',
                        style: const TextStyle(
                            fontSize: 32, fontWeight: FontWeight.bold)),
                    const SizedBox(height: 8),
                    Text(e.$1, style: const TextStyle(color: Colors.grey)),
                  ],
                ),
              ),
            )).toList(),
      ),
    );
  }
}
