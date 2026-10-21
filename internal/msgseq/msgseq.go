// Package msgseq 实现会话内消息序号的 64 分片锁生成器（§2.3）。
//
// 背景：IM 的核心并发瓶颈是消息序号的互斥生成，全局互斥锁在 2000 QPS 时
// 延迟可飙升至 300ms。方案：用户 ID 经 FNV-1a 哈希映射到 64 个分片，每个
// 分片独立加锁，锁竞争相对全局锁降低约 64 倍；分片队列超阈值时由编排层
// 动态拆片（v0.1 保留计数信号，扩容逻辑在 Phase 2 的 msg-svc 完整实现）。
package msgseq

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
)

// defaultShards 分片数。
const defaultShards = 64

var (
	// ErrInvalidBatch n<=0。
	ErrInvalidBatch = errors.New("msgseq: batch size must be positive")
	// ErrShardOverflow 单分片序号溢出（2^64，实际不可达）。
	ErrShardOverflow = errors.New("msgseq: shard sequence overflow")
)

// shard 单分片：独立互斥锁 + 分片内「每用户」序号 + 累计调用计数（拆片信号）。
// 注意：序号按用户（会话）维度连续，不按分片共享——多个用户可能哈希到同一分片，
// 若共用一个计数器会导致每个用户的序号出现空洞（跨用户抢占）。
type shard struct {
	mu    sync.Mutex
	seq   map[uint64]uint64 // userID → 该用户当前最大序号
	calls uint64
}

// Generator 分片锁消息序号生成器。
type Generator struct {
	shards      []shard
	splitThresh uint64 // 拆片信号阈值
}

// New 创建生成器。splitThresh<=0 时默认 1024（单分片累计调用次数）。
func New(splitThresh uint64) *Generator {
	if splitThresh == 0 {
		splitThresh = 1024
	}
	shards := make([]shard, defaultShards)
	for i := range shards {
		shards[i].seq = make(map[uint64]uint64)
	}
	return &Generator{shards: shards, splitThresh: splitThresh}
}

// Next 为指定用户生成下一条消息序号（会话内单调递增、连续无空洞）。
// 同一用户的序号在同一个分片内递增；不同用户并行无锁竞争。
func (g *Generator) Next(userID uint64) (uint64, error) {
	s := &g.shards[g.shardIndex(userID)]
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seq[userID] == ^uint64(0) {
		return 0, ErrShardOverflow
	}
	s.seq[userID]++
	s.calls++
	return s.seq[userID], nil
}

// NextBatch 批量生成 n 条连续序号（离线消息补拉场景）。
func (g *Generator) NextBatch(userID uint64, n int) ([]uint64, error) {
	if n <= 0 {
		return nil, ErrInvalidBatch
	}
	s := &g.shards[g.shardIndex(userID)]
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.seq[userID]
	if cur+uint64(n) < cur { // 溢出检测
		return nil, ErrShardOverflow
	}
	out := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		cur++
		out = append(out, cur)
	}
	s.seq[userID] = cur
	s.calls += uint64(n)
	return out, nil
}

// HotShardIDs 返回累计调用次数超过拆片阈值的分片（拆片信号，供监控/编排层消费）。
func (g *Generator) HotShardIDs() []int {
	var hot []int
	for i := range g.shards {
		s := &g.shards[i]
		s.mu.Lock()
		if s.calls > g.splitThresh {
			hot = append(hot, i)
		}
		s.mu.Unlock()
	}
	return hot
}

// Stats 返回各分片累计生成序号（分片内所有用户序号之和，运维监控用）。
func (g *Generator) Stats() map[int]uint64 {
	out := make(map[int]uint64, len(g.shards))
	for i := range g.shards {
		s := &g.shards[i]
		s.mu.Lock()
		var sum uint64
		for _, v := range s.seq {
			sum += v
		}
		out[i] = sum
		s.mu.Unlock()
	}
	return out
}

// shardIndex 用户 ID → 分片：FNV-1a 哈希后取模。
func (g *Generator) shardIndex(userID uint64) int {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], userID)
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	return int(h.Sum64() % uint64(len(g.shards)))
}

// Summary 供自测/日志输出。
func (g *Generator) Summary() string {
	stats := g.Stats()
	var total uint64
	for _, v := range stats {
		total += v
	}
	return fmt.Sprintf("shards=%d total_seq=%d hot=%v", len(stats), total, g.HotShardIDs())
}
