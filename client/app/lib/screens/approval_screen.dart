/// 审批态（默认态）：进入 App 首先看到 Agent 团队呈报的待审批事项（§7.1）。
/// 设计目标：30 秒内完成一次有效决策。
library;

import 'package:flutter/material.dart';

import '../models/approval_ticket.dart';
import '../widgets/approval_card.dart';

class ApprovalScreen extends StatefulWidget {
  const ApprovalScreen({super.key});

  @override
  State<ApprovalScreen> createState() => _ApprovalScreenState();
}

class _ApprovalScreenState extends State<ApprovalScreen> {
  // v0.1 演示数据；Phase 1 由 ApiClient.fetchPendingApprovals() 提供。
  final List<ApprovalTicket> _tickets = [
    ApprovalTicket(
      id: 't000001',
      sourceAgentId: 'procurement-agent',
      sourceAgentName: '采购',
      type: 'PAYMENT',
      state: TicketState.pending,
      summary: '购买办公椅 ×2（某电商 ¥3000）',
      detail: '已比价 3 家：最低 ¥2900（A店，评分4.8）。建议通过。',
      amountCents: 300000,
      trustZone: 'family',
      deadline: DateTime.now().add(const Duration(minutes: 28)),
    ),
    ApprovalTicket(
      id: 't000002',
      sourceAgentId: 'schedule-agent',
      sourceAgentName: '日程',
      type: 'NEGOTIATION',
      state: TicketState.pending,
      summary: '接受对方管家提议：周四 15:00 线上沟通',
      detail: '对方 Agent 已提交来意（商务合作）并通过预审。双方已就时间协商一致。',
      amountCents: 0,
      trustZone: 'trusted',
      deadline: DateTime.now().add(const Duration(minutes: 55)),
    ),
    ApprovalTicket(
      id: 't000003',
      sourceAgentId: 'family-agent',
      sourceAgentName: '家庭事务',
      type: 'SKILL_CALL',
      state: TicketState.pending,
      summary: '预约水管维修：A 师傅周六上午',
      detail: '物业报修渠道报价 ¥200，A 师傅 ¥150。建议选择 A 师傅。',
      amountCents: 15000,
      trustZone: 'family',
      deadline: DateTime.now().add(const Duration(hours: 1, minutes: 40)),
    ),
    ApprovalTicket(
      id: 't000004',
      sourceAgentId: 'stranger-filter-agent',
      sourceAgentName: '陌生人过滤',
      type: 'NEGOTIATION',
      state: TicketState.pending,
      summary: '陌生人来意：某猎头公司请求联系（回复才扣费 ¥50）',
      detail: '来意已预审通过（工作相关）。对方 Agent 已确认付费模式。',
      amountCents: 5000,
      trustZone: 'stranger',
      deadline: DateTime.now().add(const Duration(hours: 2)),
    ),
  ];

  void _act(String ticketId, String action) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('$ticketId → $action（Phase 1 提交至 approval-engine）')),
    );
    setState(() {
      _tickets.removeWhere((t) => t.id == ticketId);
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('审批'),
        actions: [
          IconButton(
            icon: const Icon(Icons.filter_list),
            onPressed: () {},
            tooltip: '按 紧急/重要 排序',
          ),
        ],
      ),
      body: _tickets.isEmpty
          ? const Center(child: Text('没有待审批事项，Agent 团队运转正常'))
          : ListView.builder(
              itemCount: _tickets.length,
              itemBuilder: (context, i) {
                final t = _tickets[i];
                return ApprovalCard(
                  ticket: t,
                  onApprove: () => _act(t.id, '通过'),
                  onModify: () => _act(t.id, '修改'),
                  onReject: () => _act(t.id, '驳回'),
                );
              },
            ),
    );
  }
}
