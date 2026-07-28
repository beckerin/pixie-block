package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	cfg := loadConfig()
	log.Printf("autoscaler: service=%s replicas=%d..%d up_rps=%.1f down_rps=%.1f",
		cfg.scaleService, cfg.minReplicas, cfg.maxReplicas, cfg.scaleUpRPS, cfg.scaleDownRPS)

	client := &http.Client{Timeout: 10 * time.Second}
	var (
		lastTotal   float64
		lastSample  time.Time
		replicas    = -1
		lastScale   time.Time
		initialized bool
	)

	ticker := time.NewTicker(cfg.evalInterval)
	defer ticker.Stop()

	for {
		if err := tick(client, cfg, &lastTotal, &lastSample, &replicas, &lastScale, &initialized); err != nil {
			log.Printf("autoscaler: %v", err)
		}
		<-ticker.C
	}
}

type config struct {
	composeFile        string
	projectName        string
	scaleService       string
	metricsURL         string
	metricsServiceSub  string
	minReplicas        int
	maxReplicas        int
	scaleUpRPS         float64
	scaleDownRPS       float64
	evalInterval       time.Duration
	cooldown           time.Duration
}

func loadConfig() config {
	return config{
		composeFile:       env("COMPOSE_FILE", "/workspace/compose.yaml"),
		projectName:       env("COMPOSE_PROJECT_NAME", "pixie-block"),
		scaleService:      env("SCALE_SERVICE", "follower"),
		metricsURL:        env("TRAEFIK_METRICS_URL", "http://traefik:8082/metrics"),
		metricsServiceSub: env("METRICS_SERVICE_SUBSTR", "pixie-api"),
		minReplicas:       envInt("MIN_REPLICAS", 1),
		maxReplicas:       envInt("MAX_REPLICAS", 5),
		scaleUpRPS:        envFloat("SCALE_UP_RPS", 50),
		scaleDownRPS:      envFloat("SCALE_DOWN_RPS", 10),
		evalInterval:      envDuration("EVAL_INTERVAL", 15*time.Second),
		cooldown:          envDuration("COOLDOWN", 60*time.Second),
	}
}

func tick(client *http.Client, cfg config, lastTotal *float64, lastSample *time.Time, replicas *int, lastScale *time.Time, initialized *bool) error {
	total, err := fetchRequestTotal(client, cfg.metricsURL, cfg.metricsServiceSub)
	if err != nil {
		return err
	}

	if *replicas < 0 {
		n, err := countReplicas(cfg)
		if err != nil {
			return fmt.Errorf("count replicas: %w", err)
		}
		*replicas = n
		log.Printf("autoscaler: current replicas=%d", *replicas)
	}

	now := time.Now()
	hadRPS := *initialized && !lastSample.IsZero()
	var rps float64
	if hadRPS {
		elapsed := now.Sub(*lastSample).Seconds()
		if elapsed > 0 && total >= *lastTotal {
			rps = (total - *lastTotal) / elapsed
		}
	}
	*lastTotal = total
	*lastSample = now
	*initialized = true

	log.Printf("autoscaler: requests_total=%.0f rps=%.2f replicas=%d", total, rps, *replicas)

	if now.Sub(*lastScale) < cfg.cooldown {
		return nil
	}

	target := *replicas
	switch {
	case hadRPS && rps >= cfg.scaleUpRPS && *replicas < cfg.maxReplicas:
		target = *replicas + 1
	case hadRPS && rps <= cfg.scaleDownRPS && *replicas > cfg.minReplicas:
		target = *replicas - 1
	default:
		return nil
	}

	if target == *replicas {
		return nil
	}

	if err := scaleService(cfg, target); err != nil {
		return fmt.Errorf("scale to %d: %w", target, err)
	}
	*replicas = target
	*lastScale = now
	log.Printf("autoscaler: scaled %s to %d (rps=%.2f)", cfg.scaleService, target, rps)
	return nil
}

func fetchRequestTotal(client *http.Client, metricsURL, serviceSubstr string) (float64, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, metricsURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("metrics status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	return sumTraefikServiceRequests(data, serviceSubstr)
}

func sumTraefikServiceRequests(body []byte, serviceSubstr string) (float64, error) {
	var total float64
	found := false
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "traefik_service_requests_total") {
			continue
		}
		if serviceSubstr != "" && !strings.Contains(line, serviceSubstr) {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(parts[len(parts)-1], 64)
		if err != nil {
			continue
		}
		total += v
		found = true
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return total, nil
}

func countReplicas(cfg config) (int, error) {
	out, err := dockerCompose(cfg, "ps", "-q", cfg.scaleService)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	if n == 0 {
		return cfg.minReplicas, nil
	}
	return n, nil
}

func scaleService(cfg config, replicas int) error {
	scaleArg := fmt.Sprintf("%s=%d", cfg.scaleService, replicas)
	_, err := dockerCompose(cfg, "up", "-d", "--no-recreate", "--scale", scaleArg)
	return err
}

func dockerCompose(cfg config, args ...string) ([]byte, error) {
	cmdArgs := []string{"compose", "-f", cfg.composeFile, "-p", cfg.projectName}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Dir = "/workspace"
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+env("DOCKER_HOST", ""))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return math.Max(0, n)
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
