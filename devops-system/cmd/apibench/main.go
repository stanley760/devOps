// apibench 走完整业务链路的 HTTP 压测工具，验证系统是否满足性能指标。
//
// 与 idbench 的分工：
//   - idbench：直连 devops-id gRPC，测发号服务自身的吞吐上限；
//   - apibench：打 devops-system 业务写路径（登录 → JWT → Casbin → idclient 预取 →
//     devops-id 发号 → MySQL 落库），测端到端真实性能，并让 Grafana 的
//     HTTP RED / 发号缓冲 / SQL 连接池 / 登录业务指标全部联动起来。
//
// 使用示例（在仓库根目录运行）：
//
//	# 20 并发持续创建用户 30 秒（消耗 ID，驱动缓冲补充），直连后端
//	go run ./devops-system/cmd/apibench -c 20 -d 30s
//
//	# 经 Higress 网关压测（边缘 + 后端全链路）
//	go run ./devops-system/cmd/apibench -addr http://127.0.0.1:8080 -host api.devops.local -c 20 -d 30s
//
//	# 压测登录接口（bcrypt 校验为主要开销；注意每个成功登录都会创建一个会话）
//	go run ./devops-system/cmd/apibench -mode login -c 20 -d 30s
//
// SLO 判定（与 deploy/prometheus/alerts.yml 告警阈值同源，可按需调整）：
//   - 业务写路径 P99 延迟 ≤ 1s（DevOpsHTTPP99High）
//   - 失败率（HTTP 非 200 或业务 code 非 200）≤ 1%（DevOpsHTTP5xxRateHigh 口径）
//   压测期间另在 Grafana 交叉观察：idgen_buffer_remaining 不触零、
//   idgen_refill_failures_total 为零、SQL 连接池无等待（DevOpsSQLPoolWaiting）。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// envelope 统一响应信封（只取判定所需字段）。
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// loginData data.access_token。
type loginData struct {
	AccessToken string `json:"access_token"`
}

func main() {
	addr := flag.String("addr", "http://127.0.0.1:9999", "devops-system 基地址（经网关时填网关地址并配 -host）")
	host := flag.String("host", "", "HTTP Host 头（经网关时为 api.devops.local；直连留空）")
	mode := flag.String("mode", "create", "压测模式：create（创建用户，消耗 ID）| login（登录）| mixed（各半）")
	username := flag.String("username", "admin", "登录账号（须有 system:user:create 权限）")
	password := flag.String("password", "Admin@123", "登录密码")
	tenant := flag.String("tenant", "default", "租户编码")
	dept := flag.String("dept", "101", "创建用户的 deptId（须已存在）")
	prefix := flag.String("prefix", "b_", "压测用户名前缀（自动拼接时间戳+序号保证唯一，总长 ≤30）")
	conc := flag.Int("c", 20, "并发 goroutine 数")
	dur := flag.Duration("d", 20*time.Second, "压测时长（0 表示只按 -n 总请求数跑）")
	total := flag.Int64("n", 0, "总请求数上限（0 表示只按 -d 时长跑）")
	p99SLO := flag.Duration("p99-slo", time.Second, "SLO：P99 延迟阈值")
	errSLO := flag.Float64("err-slo", 0.01, "SLO：失败率阈值（0-1）")
	timeout := flag.Duration("timeout", 10*time.Second, "单请求超时")
	flag.Parse()

	if *mode != "create" && *mode != "login" && *mode != "mixed" {
		fmt.Fprintf(os.Stderr, "apibench: -mode 仅支持 create | login | mixed\n")
		os.Exit(2)
	}
	if *dur <= 0 && *total <= 0 {
		fmt.Fprintf(os.Stderr, "apibench: -d 与 -n 至少给一个\n")
		os.Exit(2)
	}

	// 连接池：默认 Transport 每主机仅 2 条空闲连接，压测必先放大
	transport := &http.Transport{
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 512,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: *timeout}

	// 登录一次拿 Bearer token（同时充当连接预热；重试容忍启动抖动）
	var token string
	var lastErr error
	for i := 0; i < 5; i++ {
		token, lastErr = login(client, *addr, *host, *tenant, *username, *password)
		if lastErr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if lastErr != nil {
		fmt.Fprintf(os.Stderr, "apibench: 登录失败: %v\n", lastErr)
		os.Exit(1)
	}
	fmt.Printf("登录成功（%s），开始压测：mode=%s 并发=%d\n", *addr, *mode, *conc)
	if *mode == "login" {
		fmt.Println("注意: login 模式每个成功请求都会创建一个 Redis 会话")
	}
	if *mode == "create" {
		fmt.Println("注意: create 模式会真实写入用户数据（bench 前缀可事后批量删除）")
	}

	var (
		sent, okN atomic.Int64
		failN     atomic.Int64
		latMu     sync.Mutex
		latencies []time.Duration
		detailMu  sync.Mutex
		details   = map[string]int64{}
	)

	recordDetail := func(k string) {
		detailMu.Lock()
		details[k]++
		detailMu.Unlock()
	}

	startAt := time.Now()
	deadline := startAt.Add(*dur)
	stopped := func() bool {
		if *total > 0 && sent.Load() >= *total {
			return true
		}
		return *dur > 0 && time.Now().After(deadline)
	}

	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		last, lastAt := int64(0), startAt
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		fmt.Printf("时间 \t总请求 \t成功 \t失败 \t瞬时\n")
		for range ticker.C {
			now := time.Now()
			cur := sent.Load()
			fmt.Printf("  %6.1fs  %7d  %7d  %5d  %.0f/s\n",
				time.Since(startAt).Seconds(), cur, okN.Load(), failN.Load(),
				float64(cur-last)/now.Sub(lastAt).Seconds())
			last, lastAt = cur, now
		}
	}()

	var seq atomic.Int64
	tsBase := time.Now().Unix()
	var wg sync.WaitGroup
	for i := 0; i < *conc; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			local := make([]time.Duration, 0, 1024)
			defer func() {
				latMu.Lock()
				latencies = append(latencies, local...)
				latMu.Unlock()
			}()
			for !stopped() {
				n := seq.Add(1)
				var (
					path, body string
					withToken  bool
				)
				switch *mode {
				case "login":
					path, body, withToken = "/api/v1/auth/login",
						fmt.Sprintf(`{"tenant_code":%q,"username":%q,"password":%q}`, *tenant, *username, *password), false
				case "create":
					path, body, withToken = "/api/v1/users",
						fmt.Sprintf(`{"username":"%s%d_%d","password":"Bench@123456","nickname":"bench","deptId":%q}`,
							*prefix, tsBase, n, *dept), true
				default: // mixed：单双交替
					if n%2 == 0 {
						path, body, withToken = "/api/v1/auth/login",
							fmt.Sprintf(`{"tenant_code":%q,"username":%q,"password":%q}`, *tenant, *username, *password), false
					} else {
						path, body, withToken = "/api/v1/users",
							fmt.Sprintf(`{"username":"%s%d_%d","password":"Bench@123456","nickname":"bench","deptId":%q}`,
								*prefix, tsBase, n, *dept), true
					}
				}

				t0 := time.Now()
				status, code, err := doPost(client, *addr, *host, path, body, token, withToken)
				local = append(local, time.Since(t0))
				sent.Add(1)
				if err != nil {
					failN.Add(1)
					recordDetail("transport_err:" + errClass(err))
					continue
				}
				if status == http.StatusOK && code == 200 {
					okN.Add(1)
					continue
				}
				failN.Add(1)
				recordDetail("http_" + strconv.Itoa(status) + "_code_" + strconv.Itoa(code))
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(startAt)
	close(progressDone)

	// 汇总
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	sentN := sent.Load()
	fail := failN.Load()
	fmt.Println()
	fmt.Printf("======== 压测结果 ========\n")
	fmt.Printf("目标 %s（mode=%s）并发 %d  时长 %.1fs\n", *addr, *mode, *conc, elapsed.Seconds())
	fmt.Printf("总请求 %d  成功 %d  失败 %d (%.2f%%)\n", sentN, okN.Load(), fail, pct(fail, sentN))
	if elapsed.Seconds() > 0 {
		fmt.Printf("QPS %.1f\n", float64(sentN)/elapsed.Seconds())
	}
	if n := len(latencies); n > 0 {
		var sum time.Duration
		for _, d := range latencies {
			sum += d
		}
		p99 := pctile(latencies, 0.99)
		fmt.Printf("延迟  avg %s  P50 %s  P90 %s  P95 %s  P99 %s  P99.9 %s  max %s\n",
			round(sum/time.Duration(n)),
			round(pctile(latencies, 0.50)), round(pctile(latencies, 0.90)), round(pctile(latencies, 0.95)),
			round(p99), round(pctile(latencies, 0.999)), round(latencies[n-1]))
		if len(details) > 0 {
			fmt.Printf("失败明细: ")
			for k, v := range details {
				fmt.Printf("%s=%d ", k, v)
			}
			fmt.Println()
		}

		// ---- SLO 判定 ----
		fmt.Println()
		fmt.Printf("======== SLO 判定 ========\n")
		passP99 := p99 <= *p99SLO
		fmt.Printf("[%s] P99 延迟 %s ≤ %s\n", verdict(passP99), round(p99), round(*p99SLO))
		errRate := 1.0
		if sentN > 0 {
			errRate = float64(fail) / float64(sentN)
		}
		passErr := errRate <= *errSLO
		fmt.Printf("[%s] 失败率 %.2f%% ≤ %.2f%%\n", verdict(passErr), errRate*100, *errSLO*100)
		fmt.Println()
		fmt.Println("另请在 Grafana 交叉确认（压测窗口内）：")
		fmt.Println("  - idgen_buffer_remaining 未触 0（发号缓冲未耗尽）")
		fmt.Println("  - idgen_refill_failures_total 无序列（零补充失败）")
		fmt.Println("  - go_sql_wait_count_total 无增长（SQL 连接池无等待）")
		if !passP99 || !passErr {
			os.Exit(1)
		}
	}
}

// login 调用登录接口，返回 access_token。
func login(client *http.Client, addr, host, tenant, username, password string) (string, error) {
	body := fmt.Sprintf(`{"tenant_code":%q,"username":%q,"password":%q}`, tenant, username, password)
	status, code, raw, err := doPostRaw(client, addr, host, "/api/v1/auth/login", body, "", false)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || code != 200 {
		return "", fmt.Errorf("登录响应异常: http=%d code=%d body=%.200s", status, code, raw)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	var data loginData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return "", err
	}
	if data.AccessToken == "" {
		return "", fmt.Errorf("响应缺少 access_token: %.200s", raw)
	}
	return data.AccessToken, nil
}

// doPost 返回 (http状态, 业务code, error)。
func doPost(client *http.Client, addr, host, path, body, token string, withToken bool) (int, int, error) {
	status, code, _, err := doPostRaw(client, addr, host, path, body, token, withToken)
	return status, code, err
}

// doPostRaw 发送请求并解析信封 code（失败响应可能没有 JSON body，code 记 0）。
func doPostRaw(client *http.Client, addr, host, path, body, token string, withToken bool) (int, int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, addr+path, bytes.NewReader([]byte(body)))
	if err != nil {
		return 0, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	}
	if withToken {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, 0, nil, err
	}
	var env envelope
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env.Code, raw, nil
}

func pctile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

func round(d time.Duration) time.Duration {
	switch {
	case d > time.Millisecond:
		return d.Round(100 * time.Microsecond)
	case d > time.Microsecond:
		return d.Round(100 * time.Nanosecond)
	default:
		return d
	}
}

func pct(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func verdict(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

// errClass 传输层错误归类（连接被拒/超时等）。
func errClass(err error) string {
	s := err.Error()
	switch {
	case bytes.Contains([]byte(s), []byte("connection refused")):
		return "connection_refused"
	case bytes.Contains([]byte(s), []byte("Client.Timeout")):
		return "client_timeout"
	default:
		return "other"
	}
}