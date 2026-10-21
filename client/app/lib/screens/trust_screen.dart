/// 信任授权屏（v0.1.5）：手机端给目标 userID 授五档信任。
library;

import 'package:flutter/material.dart';

import '../services/api_client.dart';

class TrustScreen extends StatefulWidget {
  const TrustScreen({super.key, this.client});

  final ApiClient? client;

  @override
  State<TrustScreen> createState() => _TrustScreenState();
}

class _TrustScreenState extends State<TrustScreen> {
  late final ApiClient _client = widget.client ?? ApiClient();
  static const _levelNames = ['L0 本人', 'L1 家庭', 'L2 信任', 'L3 已知', 'L4 陌生'];

  List<Map<String, dynamic>> _trusts = [];
  final _targetCtrl = TextEditingController(text: '200');
  int _level = 1;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final t = await _client.fetchTrusts();
      if (mounted) setState(() => _trusts = t);
    } catch (_) {/* 网络未就绪时保留空列表 */}
  }

  Future<void> _grant() async {
    final target = int.tryParse(_targetCtrl.text.trim());
    if (target == null) return;
    setState(() => _saving = true);
    try {
      await _client.grantTrust(targetId: target, level: _level);
      await _refresh();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('授权失败: $e')));
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('信任授权')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('新增授权', style: TextStyle(fontWeight: FontWeight.bold)),
                  const SizedBox(height: 8),
                  TextField(
                    controller: _targetCtrl,
                    keyboardType: TextInputType.number,
                    decoration: const InputDecoration(labelText: '目标 userID'),
                  ),
                  const SizedBox(height: 8),
                  DropdownButton<int>(
                    value: _level,
                    items: [
                      for (var i = 0; i < _levelNames.length; i++)
                        DropdownMenuItem(value: i, child: Text(_levelNames[i])),
                    ],
                    onChanged: (v) => setState(() => _level = v ?? 1),
                  ),
                  const SizedBox(height: 8),
                  FilledButton(
                    onPressed: _saving ? null : _grant,
                    child: Text(_saving ? '授权中…' : '授权'),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),
          const Text('已授权列表', style: TextStyle(fontWeight: FontWeight.bold)),
          const SizedBox(height: 8),
          if (_trusts.isEmpty)
            const Padding(
              padding: EdgeInsets.all(16),
              child: Text('暂无授权', style: TextStyle(color: Colors.grey)),
            )
          else
            ..._trusts.map((t) => Card(
                  child: ListTile(
                    leading: const Icon(Icons.verified_user_outlined),
                    title: Text('target ${t['target_id']}'),
                    trailing: Chip(
                      label: Text(_levelNames[(t['level'] as int?) ?? 4]),
                    ),
                  ),
                )),
        ],
      ),
    );
  }
}
