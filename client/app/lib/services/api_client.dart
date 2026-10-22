/// API 客户端（v0.1 桩）。
///
/// Phase 1 对接：Agent 编排 API（审批/协商/信任/付费），底层 gRPC + 腾讯云 IM SDK。
/// 客户端 SDK 分层见技术文档 §5.1：UI → Business Controller → SDK Adapter → Local Persistence。
library;

class ApiClient {
  ApiClient({this.baseUrl = 'http://localhost:8080'});

  final String baseUrl;

  /// 拉取待审批列表（审批态）。Phase 1 对接 approval-engine gRPC。
  Future<List<Map<String, dynamic>>> fetchPendingApprovals() async {
    // TODO(phase1): GET $baseUrl/v1/approvals/pending
    throw UnimplementedError('Phase 1: 对接 approval-engine');
  }

  /// 提交审批决策（通过/驳回/修改）。Phase 1 对接 approval-engine gRPC。
  Future<void> submitApproval({
    required String ticketId,
    required String action, // approve / reject / modify
    String? note,
  }) async {
    // TODO(phase1): POST $baseUrl/v1/approvals/{ticketId}/{action}
    throw UnimplementedError('Phase 1: 对接 approval-engine');
  }

  /// 拉取巡视态 Agent 团队状态。Phase 1 对接 agent-orchestrator gRPC。
  Future<List<Map<String, dynamic>>> fetchAgentTeamStatus() async {
    // TODO(phase1): GET $baseUrl/v1/agents/team
    throw UnimplementedError('Phase 1: 对接 agent-orchestrator');
  }

  /// 拉取陌生人来意队列。Phase 1 对接 negotiation-svc + payment-svc。
  Future<List<Map<String, dynamic>>> fetchStrangerIntents() async {
    // TODO(phase1): GET $baseUrl/v1/stranger/intents
    throw UnimplementedError('Phase 1: 对接 negotiation-svc');
  }
}
