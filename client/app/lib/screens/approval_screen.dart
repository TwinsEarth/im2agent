/// 审批态（默认态）：进入 App 首先看到 Agent 团队呈报的待审批事项。
/// 真连后端 /api/admin/state 与 /api/approval/decide。
library;

import 'dart:async';

import 'package:flutter/material.dart';

import '../models/approval_ticket.dart';
import '../services/api_client.dart';
import '../widgets/approval_card.dart';

class ApprovalScreen extends StatefulWidget {
  const ApprovalScreen({super.key});

  @override
  State<ApprovalScreen> createState() => _ApprovalScreenState();
}

class _ApprovalScreenState extends State<ApprovalScreen> {
  final _client = ApiClient();
  List<ApprovalTicket> _tickets = [];
  bool _loading = true;
  String? _error;
  StreamSubscription? _sseSub;
  Timer? _pollTimer; // v0.1.4：SSE 断连回退的 5s 轮询

  @override
  void initState() {
    super.initState();
    _refresh();
    // v0.1.4：SSE 事件驱动刷新；断连后 5s 轮询兜底。
    _startSse();
  }

  void _startSse() {
    _sseSub = _client.streamEvents().listen(
      (_) => _refresh(),
      onError: (_) => _fallbackPolling(),
      onDone: _fallbackPolling,
    );
  }

  void _fallbackPolling() {
    _pollTimer ??= Timer.periodic(const Duration(seconds: 5), (_) => _refresh());
  }

  @override
  void dispose() {
    _sseSub?.cancel();
    _pollTimer?.cancel();
    super.dispose();
  }

  Future<void> _refresh() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final raw = await _client.fetchPendingApprovals();
      setState(() {
        _tickets = raw.map(_fromRaw).toList();
        _loading = false;
      });
    } catch (e) {
      setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  ApprovalTicket _fromRaw(Map<String, dynamic> p) {
    final payload = (p['payload'] as Map<String, dynamic>?) ?? {};
    return ApprovalTicket(
      id: p['id'] as String,
      sourceAgentId: p['from'] as String? ?? 'unknown',
      sourceAgentName: (p['from'] as String? ?? 'unknown'),
      type: p['type'] as String? ?? 'PAYMENT',
      state: TicketState.pending,
      summary: (payload['item'] as String?) ??
          (payload['category'] as String? ?? '待审批事项'),
      detail: (payload['reason'] as String?) ??
          (payload['content'] as String? ?? ''),
      amountCents: (payload['amountCents'] as num?)?.toInt() ?? 0,
      trustZone: (payload['isStranger'] == true) ? 'stranger' : 'family',
      deadline: DateTime.tryParse(p['deadline'] as String? ?? '') ??
          DateTime.now().add(const Duration(minutes: 30)),
    );
  }

  Future<void> _act(String ticketId, String action) async {
    try {
      await _client.submitApproval(ticketId: ticketId, action: action);
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('$ticketId → $action 成功')));
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('失败：$e')));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('审批'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _refresh,
            tooltip: '刷新',
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.cloud_off, size: 48, color: Colors.grey),
              const SizedBox(height: 12),
              Text('连不上后端：$_error', textAlign: TextAlign.center),
              const SizedBox(height: 12),
              FilledButton(
                onPressed: _refresh,
                child: const Text('重试'),
              ),
            ],
          ),
        ),
      );
    }
    if (_tickets.isEmpty) {
      return const Center(child: Text('没有待审批事项，Agent 团队运转正常'));
    }
    return ListView.builder(
      itemCount: _tickets.length,
      itemBuilder: (context, i) {
        final t = _tickets[i];
        return ApprovalCard(
          ticket: t,
          onApprove: () => _act(t.id, 'approve'),
          onModify: () => _act(t.id, 'modify'),
          onReject: () => _act(t.id, 'reject'),
        );
      },
    );
  }
}
