// msg-svc：消息序号生成服务（§2.3）。
// 核心：64 分片锁 + FNV 哈希，避免全局互斥锁瓶颈。
// 运行：go run ./services/msg-svc -selftest   # 自测后退出
//
//	go run ./services/msg-svc -port 8081  # 启动 healthz
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"

	"human-agent-im/internal/api"
	"human-agent-im/internal/config"
	"human-agent-im/internal/msgseq"
)

func main() {
	selftest := flag.Bool("selftest", false, "run self test and exit")
	cfg := config.Load("msg-svc")

	if *selftest {
		if err := selfTest(); err != nil {
			log.Fatalf("[msg-svc] SELF-TEST FAILED: %v", err)
		}
		return
	}

	mux := api.NewMsgMux(cfg.ServiceName)
	log.Printf("[msg-svc] listening on %s (env=%s)", cfg.Addr(), cfg.Env)
	log.Fatal(http.ListenAndServe(cfg.Addr(), mux))
}

// selfTest 并发验证：100 用户 × 100 消息，断言总量与单用户序号严格递增且无重复。
func selfTest() error {
	const users = 100
	const msgsPerUser = 100

	g := msgseq.New(1024)

	var wg sync.WaitGroup
	perUser := make([][]uint64, users)
	for u := 0; u < users; u++ {
		u := u
		perUser[u] = make([]uint64, 0, msgsPerUser)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := 0; m < msgsPerUser; m++ {
				seq, err := g.Next(uint64(u + 1))
				if err != nil {
					log.Printf("[msg-svc] Next error: %v", err)
					return
				}
				perUser[u] = append(perUser[u], seq)
			}
		}()
	}
	wg.Wait()

	// 断言：总量正确
	total := 0
	for u := 0; u < users; u++ {
		total += len(perUser[u])
	}
	if total != users*msgsPerUser {
		return fmt.Errorf("total seq = %d, want %d", total, users*msgsPerUser)
	}
	// 断言：单用户序号连续且唯一
	for u := 0; u < users; u++ {
		seqs := perUser[u]
		sorted := append([]uint64(nil), seqs...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		seen := map[uint64]bool{}
		for i, s := range sorted {
			if seen[s] {
				return fmt.Errorf("user %d duplicate seq %d", u+1, s)
			}
			seen[s] = true
			if i > 0 && s != sorted[i-1]+1 {
				return fmt.Errorf("user %d seq gap: %d -> %d", u+1, sorted[i-1], s)
			}
		}
	}

	fmt.Printf("[msg-svc] SELF-TEST PASS: %d users × %d msgs, total=%d, %s\n",
		users, msgsPerUser, total, g.Summary())
	return nil
}
