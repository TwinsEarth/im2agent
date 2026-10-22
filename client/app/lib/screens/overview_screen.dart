/// 巡视态：各子 Agent 工作状态可视化（§7.1）。
/// 可钻入任一子 Agent 查看完整执行轨迹（Phase 1：决策轨迹回放）。
library;

import 'package:flutter/material.dart';

import '../models/agent_status.dart';

class OverviewScreen extends StatelessWidget {
  const OverviewScreen({super.key});

  // v0.1 演示数据；Phase 1 由 ApiClient.fetchAgentTeamStatus() 提供。
  static const List<AgentStatus> _agents = [
    AgentStatus(
      agentId: 'master-agent',
      name: '管家',
      description: '任务路由 / 优先级仲裁 / 呈报',
      state: AgentWorkState.working,
      activity: '正在协调 3 个子 Agent 的今日事务',
      pendingApprovals: 0,
      todayActions: 12,
    ),
    AgentStatus(
      agentId: 'procurement-agent',
      name: '采购',
      description: '比价 / 下单',
      state: AgentWorkState.needsApproval,
      activity: '办公椅比价完成，等待审批',
      pendingApprovals: 1,
      todayActions: 3,
    ),
    AgentStatus(
      agentId: 'schedule-agent',
      name: '日程',
      description: '预约 / 日程协调',
      state: AgentWorkState.needsApproval,
      activity: '周四 15:00 沟通待确认',
      pendingApprovals: 1,
      todayActions: 5,
    ),
    AgentStatus(
      agentId: 'family-agent',
      name: '家庭事务',
      description: '物业 / 水电 / 家政 / 维修',
      state: AgentWorkState.working,
      activity: '对比 2 家维修报价中',
      pendingApprovals: 1,
      todayActions: 4,
    ),
    AgentStatus(
      agentId: 'stranger-filter-agent',
      name: '陌生人过滤',
      description: '来意预审 / 付费门控',
      state: AgentWorkState.idle,
      activity: '今日拦截 7 条询盘，放行 2 条',
      pendingApprovals: 1,
      todayActions: 9,
    ),
  ];

  Color _colorOf(AgentWorkState s) {
    switch (s) {
      case AgentWorkState.working:
        return Colors.blue;
      case AgentWorkState.needsApproval:
        return Colors.orange;
      case AgentWorkState.blocked:
        return Colors.red;
      case AgentWorkState.idle:
        return Colors.green;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Agent 团队')),
      body: ListView.builder(
        itemCount: _agents.length,
        itemBuilder: (context, i) {
          final a = _agents[i];
          return Card(
            margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
            child: ListTile(
              leading: CircleAvatar(
                backgroundColor: _colorOf(a.state).withValues(alpha: 0.15),
                child: Icon(Icons.smart_toy_outlined,
                    color: _colorOf(a.state)),
              ),
              title: Text('${a.name}  ·  ${a.state.label}'),
              subtitle: Text('${a.description}\n${a.activity}'),
              trailing: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  if (a.pendingApprovals > 0)
                    Text('待审 ${a.pendingApprovals}',
                        style: TextStyle(
                            color: Colors.orange.shade800,
                            fontWeight: FontWeight.bold)),
                  Text('今日 ${a.todayActions} 项',
                      style: Theme.of(context).textTheme.labelSmall),
                ],
              ),
              onTap: () {
                // Phase 1：钻入子 Agent 查看完整执行轨迹（§8.3 决策轨迹回放）
                ScaffoldMessenger.of(context).showSnackBar(
                  SnackBar(content: Text('${a.agentId} 执行轨迹（Phase 1 开放）')),
                );
              },
            ),
          );
        },
      ),
    );
  }
}
