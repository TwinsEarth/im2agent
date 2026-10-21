/// 账本态：陌生人付费消息扣费记录（§1.3）。
///
/// 数据源：GET /api/admin/ledger（entries / dailySent / dailyLimit）。
library;

import 'package:flutter/material.dart';

import '../services/api_client.dart';

class LedgerScreen extends StatefulWidget {
  const LedgerScreen({super.key, this.client = const ApiClient()});

  final ApiClient client;

  @override
  State<LedgerScreen> createState() => _LedgerScreenState();
}

class _LedgerScreenState extends State<LedgerScreen> {
  late Future<Map<String, dynamic>> _future = widget.client.fetchLedger();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('账本 · 陌生人付费')),
      body: RefreshIndicator(
        onRefresh: () async {
          setState(() => _future = widget.client.fetchLedger());
        },
        child: FutureBuilder<Map<String, dynamic>>(
          future: _future,
          builder: (context, snap) {
            if (snap.connectionState == ConnectionState.waiting) {
              return const Center(child: CircularProgressIndicator());
            }
            if (snap.hasError) {
              return ListView(children: [
                const SizedBox(height: 80),
                Center(child: Text('后端未连接：${snap.error}')),
                const SizedBox(height: 16),
                const Center(child: Text('启动 cmd/demo 后下拉刷新')),
              ]);
            }
            final data = snap.data ?? {};
            final entries = (data['entries'] as List? ?? [])
                .cast<Map<String, dynamic>>();
            final sent = data['dailySent'] ?? 0;
            final limit = data['dailyLimit'] ?? 10;
            return ListView(
              padding: const EdgeInsets.all(12),
              children: [
                Card(
                  child: ListTile(
                    title: const Text('今日发送 / 上限'),
                    trailing: Text('$sent / $limit',
                        style: Theme.of(context).textTheme.titleMedium),
                  ),
                ),
                const SizedBox(height: 8),
                if (entries.isEmpty)
                  const Padding(
                    padding: EdgeInsets.only(top: 60),
                    child: Center(child: Text('暂无扣费记录')),
                  )
                else
                  ...entries.map((e) => Card(
                        child: ListTile(
                          leading: Icon(
                            e['status'] == 'Charged'
                                ? Icons.check_circle_outline
                                : Icons.replay_circle_filled,
                            color: e['status'] == 'Charged'
                                ? Colors.green
                                : Colors.red,
                          ),
                          title: Text(
                              '¥${((e['amountCents'] ?? 0) as num) / 100} · ${e['mode'] ?? ''}'),
                          subtitle: Text(
                              '${e['senderId']} → ${e['receiverId']} · ${e['status']}'),
                          trailing: Text(
                            e['chargedAt']?.toString().substring(0, 16) ?? '-',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ),
                      )),
              ],
            );
          },
        ),
      ),
    );
  }
}
