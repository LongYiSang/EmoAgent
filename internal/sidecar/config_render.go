package sidecar

import (
	"bytes"
	"fmt"
	"strings"
)

func RenderConfig(spec Spec) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeSection(&b, "trivium")
	writeString(&b, "dir", spec.TriviumDir)
	writeString(&b, "dtype", "f32")
	writeString(&b, "sync_mode", "normal")

	writeSection(&b, "embedding")
	writeProvider(&b, spec.Embedding)
	if spec.Embedding.Dimensions > 0 {
		writeInt(&b, "dimensions", spec.Embedding.Dimensions)
	}
	writeInt(&b, "timeout_seconds", 30)
	writeString(&b, "encoding_format", "float")

	writeSection(&b, "embedding_cache")
	writeString(&b, "mode", "read_write")
	writeString(&b, "db_path", spec.EmbeddingCacheDBPath)
	writeString(&b, "text_normalization_version", "v1")
	writeString(&b, "searchable_text_version", "v1")
	writeInt(&b, "ttl_days_for_query", 30)
	writeBool(&b, "store_raw_text", false)

	writeSection(&b, "rerank")
	writeProviderWithOptions(&b, spec.Rerank, providerWriteOptions{EndpointURL: true})
	writeInt(&b, "timeout_seconds", 30)
	writeInt(&b, "top_n", defaultInt(spec.Rerank.TopK, 16))

	writeSection(&b, "query_analysis")
	writeProvider(&b, spec.QueryAnalysis)
	writeInt(&b, "timeout_seconds", 30)
	writeInt(&b, "max_tokens", defaultInt(spec.QueryAnalysis.MaxTokens, 768))
	writeFloat(&b, "temperature", 0)
	writeString(&b, "response_format", "json_object")
	writeString(&b, "prompt_version", "memory_query_analysis_v1")

	return b.Bytes(), nil
}

func writeSection(b *bytes.Buffer, name string) {
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "[%s]\n", name)
}

func writeProvider(b *bytes.Buffer, provider ProviderBinding) {
	writeProviderWithOptions(b, provider, providerWriteOptions{})
}

type providerWriteOptions struct {
	EndpointURL bool
}

func writeProviderWithOptions(b *bytes.Buffer, provider ProviderBinding, opts providerWriteOptions) {
	writeString(b, "provider", defaultString(provider.Provider, "none"))
	if opts.EndpointURL && strings.TrimSpace(provider.EndpointURL) != "" {
		writeString(b, "endpoint_url", provider.EndpointURL)
	} else if strings.TrimSpace(provider.BaseURL) != "" {
		writeString(b, "base_url", provider.BaseURL)
	}
	if strings.TrimSpace(provider.APIKeyEnv) != "" {
		writeString(b, "api_key_env", provider.APIKeyEnv)
	}
	if strings.TrimSpace(provider.Model) != "" {
		writeString(b, "model", provider.Model)
	}
}

func writeString(b *bytes.Buffer, key string, value string) {
	fmt.Fprintf(b, "%s = \"%s\"\n", key, tomlString(value))
}

func writeInt(b *bytes.Buffer, key string, value int) {
	fmt.Fprintf(b, "%s = %d\n", key, value)
}

func writeFloat(b *bytes.Buffer, key string, value float64) {
	fmt.Fprintf(b, "%s = %.3g\n", key, value)
}

func writeBool(b *bytes.Buffer, key string, value bool) {
	fmt.Fprintf(b, "%s = %t\n", key, value)
}

func tomlString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
