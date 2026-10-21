/// 审批卡片组件（§7.1）：哪个子 Agent 发起 → 做了什么 → 建议动作 → 信任档位 → 一键操作。
library;

import 'package:flutter/material.dart';

import '../models/approval_ticket.dart';

class ApprovalCard extends StatelessWidget {
  const ApprovalCard({
    super.key,
    required this.ticket,
    required this.onApprove,
    required this.onModify,
    required this.onReject,
  });

  final ApprovalTicket ticket;
  final VoidCallback onApprove;
  final VoidCallback onModify;
  final VoidCallback onReject;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                CircleAvatar(
                  radius: 16,
                  child: Text(ticket.sourceAgentName.characters.first),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(ticket.sourceAgentName,
                          style: theme.textTheme.titleMedium),
                      Text(ticket.summary,
                          style: theme.textTheme.bodyMedium),
                    ],
                  ),
                ),
                // v0.2.5：信任档中文标签 + 颜色区分
                Chip(
                  label: Text(
                    ticket.trustZone == 'stranger'
                        ? '陌生人 · L4'
                        : '家庭 · L1',
                    style: const TextStyle(fontSize: 11),
                  ),
                  visualDensity: VisualDensity.compact,
                  backgroundColor: ticket.trustZone == 'stranger'
                      ? Colors.orange.shade100
                      : Colors.green.shade100,
                ),
              ],
            ),
            const SizedBox(height: 10),
            Text(ticket.detail, style: theme.textTheme.bodySmall),
            const SizedBox(height: 8),
            Row(
              children: [
                Text('金额: ${ticket.amountLabel}',
                    style: theme.textTheme.labelMedium),
                const SizedBox(width: 16),
                Text('截止: ${_fmt(ticket.deadline)}',
                    style: theme.textTheme.labelMedium),
              ],
            ),
            const SizedBox(height: 12),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(onPressed: onReject, child: const Text('驳回')),
                const SizedBox(width: 8),
                OutlinedButton(
                    onPressed: onModify, child: const Text('修改')),
                const SizedBox(width: 8),
                FilledButton(onPressed: onApprove, child: const Text('通过')),
              ],
            ),
          ],
        ),
      ),
    );
  }

  static String _fmt(DateTime t) =>
      '${t.month}/${t.day} ${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
}
