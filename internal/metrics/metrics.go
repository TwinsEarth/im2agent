// Package metrics 提供 v1.9.9 Prometheus 文本格式指标：纯 stdlib，并发安全。
// 不引入外部依赖，输出符合 Prometheus text exposition format。
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

var (
	mu       sync.Mutex
	counters = map[string]map[string]int64{} // name -> labelSet -> value
	summaries = map[string][]float64{}        // name -> 延迟样本（ms）
)

// IncCounter 对带标签的计数器 +1。labelSet 如 "path=/v1/msg/send,code=201"。
func IncCounter(name, labelSet string) {
	mu.Lock()
	defer mu.Unlock()
	if counters[name] == nil {
		counters[name] = map[string]int64{}
	}
	counters[name][labelSet]++
}

// ObserveDuration 记录一次请求耗时（毫秒）。
func ObserveDuration(name string, ms float64) {
	mu.Lock()
	defer mu.Unlock()
	summaries[name] = append(summaries[name], ms)
}

// Render 输出 Prometheus 文本格式。
func Render() string {
	mu.Lock()
	defer mu.Unlock()
	out := ""
	names := make([]string, 0, len(counters))
	for n := range counters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out += fmt.Sprintf("# HELP %s 计数\n# TYPE %s counter\n", n, n)
		labels := make([]string, 0, len(counters[n]))
		for ls := range counters[n] {
			labels = append(labels, ls)
		}
		sort.Strings(labels)
		for _, ls := range labels {
			out += fmt.Sprintf("%s{%s} %d\n", n, ls, counters[n][ls])
		}
	}
	// 延迟汇总：count/avg/p99
	for n, samples := range summaries {
		if len(samples) == 0 {
			continue
		}
		sorted := make([]float64, len(samples))
		copy(sorted, samples)
		sort.Float64s(sorted)
		sum := 0.0
		for _, v := range sorted {
			sum += v
		}
		p99 := sorted[int(float64(len(sorted)-1)*0.99)]
		out += fmt.Sprintf("# HELP %s 毫秒\n# TYPE %s summary\n", n, n)
		out += fmt.Sprintf("%s_count %d\n", n, len(sorted))
		out += fmt.Sprintf("%s_avg_ms %.2f\n", n, sum/float64(len(sorted)))
		out += fmt.Sprintf("%s_p99_ms %.2f\n", n, p99)
	}
	return out
}

// Handler 返回 /metrics 端点。
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, Render())
	})
}

// Middleware 包装 handler，真实记录每个请求的计数与延迟。
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(ww, r)
		ms := float64(time.Since(start).Microseconds()) / 1000.0
		ls := fmt.Sprintf(`path=%q,code=%d`, r.URL.Path, ww.code)
		IncCounter("http_requests_total", ls)
		ObserveDuration("http_request_duration_ms", ms)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) {
	s.code = c
	s.ResponseWriter.WriteHeader(c)
}
