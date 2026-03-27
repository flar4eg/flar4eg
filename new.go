package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultFile     = "vless_universal.txt"
	subscriptionURL = "https://raw.githubusercontent.com/zieng2/wl/main/vless_universal.txt"
	timeout         = 3 * time.Second
)

type Result struct {
	Name    string
	Host    string
	Port    string
	Latency time.Duration
	OK      bool
	Line    string
}

func input(prompt string) string {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func downloadFile(src, dest string) error {
	fmt.Printf("🌐 Скачиваем с %s...\n", src)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(src)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(resp.Body)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			f.WriteString(line + "\n")
			count++
		}
	}
	fmt.Printf("✅ Сохранено %d серверов в %s\n", count, dest)
	return nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func fetchFromURL(src string) ([]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(src)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func parseLine(line string) (host, port, name string, ok bool) {
	if !strings.HasPrefix(line, "vless://") {
		return
	}
	if idx := strings.LastIndex(line, "#"); idx != -1 {
		raw := line[idx+1:]
		decoded, err := url.QueryUnescape(raw)
		if err == nil {
			name = decoded
		} else {
			name = raw
		}
	}
	if name == "" {
		name = "без имени"
	}

	withoutScheme := strings.TrimPrefix(line, "vless://")
	atIdx := strings.Index(withoutScheme, "@")
	if atIdx == -1 {
		return
	}
	afterAt := withoutScheme[atIdx+1:]
	hostPort := afterAt
	if idx := strings.Index(hostPort, "?"); idx != -1 {
		hostPort = hostPort[:idx]
	}
	if idx := strings.Index(hostPort, "#"); idx != -1 {
		hostPort = hostPort[:idx]
	}
	h, p, err := net.SplitHostPort(hostPort)
	if err != nil {
		return
	}
	host, port, ok = h, p, true
	return
}

func checkServer(line string, idx int, total int, done *int64) Result {
	host, port, name, ok := parseLine(line)

	current := atomic.AddInt64(done, 1)
	if ok {
		fmt.Printf("[%d/%d] 🔄 Проверяем: %s (%s:%s)\n", current, total, name, host, port)
	} else {
		fmt.Printf("[%d/%d] ⚠️  Пропускаем (не удалось разобрать строку)\n", current, total)
		return Result{OK: false, Name: "?", Line: line}
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf("[%d/%d] ❌ %s — недоступен (%dms)\n", current, total, name, latency.Milliseconds())
		return Result{Name: name, Host: host, Port: port, OK: false, Line: line}
	}
	conn.Close()

	fmt.Printf("[%d/%d] ✅ %s — %dms\n", current, total, name, latency.Milliseconds())
	return Result{Name: name, Host: host, Port: port, Latency: latency, OK: true, Line: line}
}

func runCheck(lines []string) []Result {
	total := len(lines)
	results := make([]Result, total)
	var wg sync.WaitGroup
	var done int64

	// Ограничиваем параллельность чтобы не спамить вывод слишком хаотично
	sem := make(chan struct{}, 20)

	for i, line := range lines {
		wg.Add(1)
		go func(idx int, l string) {
			defer wg.Done()
			sem <- struct{}{}
			results[idx] = checkServer(l, idx+1, total, &done)
			<-sem
		}(i, line)
	}
	wg.Wait()
	return results
}

func showTop(ok []Result) {
	total := len(ok)
	if total == 0 {
		fmt.Println("❌ Нет рабочих серверов")
		return
	}

	fmt.Printf("\nВсего рабочих серверов: %d\n", total)

	for {
		raw := input(fmt.Sprintf("Показать какие 5? Введи начальный номер (1-%d), или 0 для выхода: ", total))
		n, err := strconv.Atoi(raw)
		if err != nil || n == 0 {
			break
		}
		if n < 1 || n > total {
			fmt.Printf("⚠️  Введи число от 1 до %d\n", total)
			continue
		}

		end := n + 4
		if end > total {
			end = total
		}

		fmt.Printf("\n=== 🏆 Серверы с %d по %d (из %d рабочих) ===\n", n, end, total)
		for i := n - 1; i < end; i++ {
			r := ok[i]
			fmt.Printf("\n%d. %s (%s:%s) — %dms\n", i+1, r.Name, r.Host, r.Port, r.Latency.Milliseconds())
			fmt.Printf("   %s\n", r.Line)
		}
	}
}

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║      VLESS Server Checker        ║")
	fmt.Println("╚══════════════════════════════════╝")
	fmt.Println()

	localExists := false
	if _, err := os.Stat(defaultFile); err == nil {
		localExists = true
	}

	fmt.Println("Выбери источник:")
	fmt.Println("  1 — Локальный файл (vless_universal.txt)")
	fmt.Println("  2 — Скачать с URL и проверить")
	fmt.Println("  3 — Обновить локальный файл с URL (перезаписать)")
	if !localExists {
		fmt.Println("  ⚠️  Локальный файл не найден")
	}
	fmt.Println()

	choice := input("Твой выбор (1/2/3): ")

	var lines []string
	var err error

	switch choice {
	case "1":
		if !localExists {
			fmt.Println("❌ Файл vless_universal.txt не найден рядом со скриптом")
			os.Exit(1)
		}
		fmt.Printf("📂 Читаем %s...\n", defaultFile)
		lines, err = readLines(defaultFile)
		if err != nil {
			fmt.Printf("❌ Ошибка чтения файла: %v\n", err)
			os.Exit(1)
		}

	case "2":
		srcURL := input(fmt.Sprintf("URL подписки [Enter = %s]: ", subscriptionURL))
		if srcURL == "" {
			srcURL = subscriptionURL
		}
		lines, err = fetchFromURL(srcURL)
		if err != nil {
			fmt.Printf("❌ Ошибка загрузки: %v\n", err)
			os.Exit(1)
		}

	case "3":
		srcURL := input(fmt.Sprintf("URL подписки [Enter = %s]: ", subscriptionURL))
		if srcURL == "" {
			srcURL = subscriptionURL
		}
		err = downloadFile(srcURL, defaultFile)
		if err != nil {
			fmt.Printf("❌ Ошибка загрузки: %v\n", err)
			os.Exit(1)
		}
		lines, err = readLines(defaultFile)
		if err != nil {
			fmt.Printf("❌ Ошибка чтения файла: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Println("❌ Неверный выбор")
		os.Exit(1)
	}

	fmt.Printf("\n🔍 Найдено серверов: %d\n", len(lines))
	fmt.Printf("⏱️  Таймаут: %.0f сек\n\n", timeout.Seconds())

	results := runCheck(lines)

	var okResults, failResults []Result
	for _, r := range results {
		if r.OK {
			okResults = append(okResults, r)
		} else {
			failResults = append(failResults, r)
		}
	}

	sort.Slice(okResults, func(i, j int) bool {
		return okResults[i].Latency < okResults[j].Latency
	})

	fmt.Printf("\n══════════════════════════════════\n")
	fmt.Printf("✅ Рабочих:    %d\n", len(okResults))
	fmt.Printf("❌ Недоступных: %d\n", len(failResults))
	fmt.Printf("══════════════════════════════════\n")

	showTop(okResults)

	fmt.Println("\nГотово!")
}
