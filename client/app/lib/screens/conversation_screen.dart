/// 熟人 IM 会话（§7.1）：保留传统聊天界面，顶部增加"Agent 摘要"层。
/// 人机混合的落点：Agent 承担协调/预约/确认的"行政层"，人只处理情感层与最终决策层。
library;

import 'package:flutter/material.dart';

class Conversation {
  final String id;
  final String name;
  final String agentDigest; // Agent 摘要
  final String lastMessage;
  final bool unread;

  const Conversation({
    required this.id,
    required this.name,
    required this.agentDigest,
    required this.lastMessage,
    required this.unread,
  });
}

class ConversationScreen extends StatelessWidget {
  const ConversationScreen({super.key});

  static const List<Conversation> _convs = [
    Conversation(
      id: 'c-family',
      name: '家庭群',
      agentDigest: '家庭Agent：已确认周六 A 师傅上门维修水管',
      lastMessage: '妈：晚上回来吃饭吗？',
      unread: true,
    ),
    Conversation(
      id: 'c-chen',
      name: '陈明（信任）',
      agentDigest: '对方管家提议周四 15:00 线上沟通，双方已协商一致，等待你批准',
      lastMessage: '陈明：上次说的合作方案你看了吗',
      unread: true,
    ),
    Conversation(
      id: 'c-work',
      name: '项目组',
      agentDigest: '工作Agent：邮件已归档 3 封待办，会议纪要已同步',
      lastMessage: '李工：明天上午评审改到 10 点',
      unread: false,
    ),
    Conversation(
      id: 'c-alumni',
      name: '校友群',
      agentDigest: '',
      lastMessage: '王强：有人这周末去杭州吗',
      unread: false,
    ),
  ];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('会话'),
        actions: [
          IconButton(
            icon: const Icon(Icons.group_add_outlined),
            onPressed: () {},
            tooltip: '创建熟人群（家庭/公司/学校/社区/校友）',
          ),
        ],
      ),
      body: ListView.separated(
        itemCount: _convs.length,
        separatorBuilder: (_, __) => const Divider(height: 1),
        itemBuilder: (context, i) {
          final c = _convs[i];
          return ListTile(
            leading: CircleAvatar(
              child: Text(c.name.characters.first),
            ),
            title: Row(
              children: [
                Expanded(child: Text(c.name)),
                if (c.unread)
                  const Padding(
                    padding: EdgeInsets.only(left: 8),
                    child: Icon(Icons.circle, size: 10, color: Colors.red),
                  ),
              ],
            ),
            subtitle: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(c.lastMessage,
                    maxLines: 1, overflow: TextOverflow.ellipsis),
                if (c.agentDigest.isNotEmpty)
                  Container(
                    margin: const EdgeInsets.only(top: 4),
                    padding: const EdgeInsets.symmetric(
                        horizontal: 8, vertical: 4),
                    decoration: BoxDecoration(
                      color: Colors.blue.withValues(alpha: 0.08),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Row(
                      children: [
                        const Icon(Icons.smart_toy_outlined,
                            size: 14, color: Colors.blue),
                        const SizedBox(width: 4),
                        Expanded(
                          child: Text(
                            c.agentDigest,
                            style: const TextStyle(
                                fontSize: 12, color: Colors.blueGrey),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
            onTap: () {
              ScaffoldMessenger.of(context).showSnackBar(
                SnackBar(content: Text('${c.name} 会话（Phase 1 接入腾讯云 IM）')),
              );
            },
          );
        },
      ),
    );
  }
}
