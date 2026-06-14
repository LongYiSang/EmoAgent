//go:build live

package llm

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSiliconFlowLiveModelDiscovery(t *testing.T) {
	key := os.Getenv("SILICONFLOW_API_KEY")
	if strings.TrimSpace(key) == "" {
		key = loadSiliconFlowKeyFromDotEnv(t)
	}
	if strings.TrimSpace(key) == "" {
		t.Skip("SILICONFLOW_API_KEY is not set")
	}
	t.Setenv("SILICONFLOW_API_KEY", key)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := DiscoverModels(ctx, ProviderConfig{
		PresetID:       "siliconflow",
		ModelDiscovery: "siliconflow_models",
	})
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}

	counts := map[string]int{}
	samples := map[string][]string{}
	for _, model := range models {
		counts[model.SubType]++
		if len(samples[model.SubType]) < 3 {
			samples[model.SubType] = append(samples[model.SubType], model.ID)
		}
	}
	for _, subtype := range []string{"chat", "embedding", "reranker"} {
		if counts[subtype] == 0 {
			t.Fatalf("no SiliconFlow %s models discovered; counts=%#v", subtype, counts)
		}
		t.Logf("%s count=%d sample=%s", subtype, counts[subtype], strings.Join(samples[subtype], ", "))
	}
}

func loadSiliconFlowKeyFromDotEnv(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, ".env")
		file, err := os.Open(path)
		if err == nil {
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "SILICONFLOW_API_KEY=") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "SILICONFLOW_API_KEY="))
					return strings.Trim(value, `"'`)
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
