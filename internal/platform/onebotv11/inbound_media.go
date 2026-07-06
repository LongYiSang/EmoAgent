package onebotv11

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
)

type InboundMediaStore interface {
	Upload(ctx context.Context, r io.Reader, meta media.UploadMeta) (*media.MediaAsset, error)
}

func resolveInboundMedia(ctx context.Context, value RawMessageValue, cfg MessageConfig, store InboundMediaStore) ([]llm.ContentBlock, string, error) {
	if !cfg.InboundMedia.Enabled || value.IsString {
		return nil, "", nil
	}
	imageCount := 0
	for _, segment := range value.Segments {
		if segment.Type == "image" {
			imageCount++
		}
	}
	if imageCount == 0 {
		return nil, "", nil
	}
	if imageCount > cfg.InboundMedia.MaxImages {
		return nil, "", fmt.Errorf("图片数量超过限制，最多 %d 张", cfg.InboundMedia.MaxImages)
	}
	if store == nil {
		return nil, "", fmt.Errorf("图片接收服务未配置")
	}

	parts := make([]llm.ContentBlock, 0, len(value.Segments))
	for _, segment := range value.Segments {
		switch segment.Type {
		case "text":
			if text := strings.TrimSpace(segment.Data["text"]); text != "" {
				parts = append(parts, llm.ContentBlock{Type: string(llm.PartText), Text: text})
			}
		case "image":
			rawURL := strings.TrimSpace(segment.Data["url"])
			if rawURL == "" {
				return nil, "", fmt.Errorf("图片缺少可下载地址")
			}
			asset, err := downloadInboundImage(ctx, rawURL, cfg.InboundMedia, store)
			if err != nil {
				return nil, "", err
			}
			parts = append(parts, llm.ContentBlock{
				Type: string(llm.PartImage),
				Media: &llm.MediaPart{
					MediaAssetID: asset.ID,
					Kind:         firstNonEmpty(asset.Kind, "image"),
					MimeType:     asset.MimeType,
				},
			})
		default:
			if text := strings.TrimSpace(segmentPlaceholder(segment.Type, cfg)); text != "" {
				parts = append(parts, llm.ContentBlock{Type: string(llm.PartText), Text: text})
			}
		}
	}
	if len(parts) == 0 {
		return nil, "", nil
	}
	rendered := llm.RenderMessage(llm.Message{
		Role:          llm.RoleUser,
		ContentBlocks: parts,
	}, llm.RenderForHistory, llm.RenderPolicy{})
	return parts, rendered.Content, nil
}

func downloadInboundImage(ctx context.Context, rawURL string, cfg InboundMediaConfig, store InboundMediaStore) (*media.MediaAsset, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("图片地址无效")
	}
	if err := validateInboundMediaURL(parsed, cfg); err != nil {
		return nil, err
	}

	timeout := time.Duration(cfg.DownloadTimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建图片下载请求失败: %w", err)
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: inboundMediaHTTPTransport(cfg),
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return validateInboundMediaURL(req.URL, cfg)
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("图片下载失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("图片下载失败: HTTP %d", resp.StatusCode)
	}
	return store.Upload(reqCtx, io.LimitReader(resp.Body, media.DefaultMaxBytes+1), media.UploadMeta{
		OriginalFilename: inboundMediaFilename(parsed),
		CreatedByRole:    "user",
	})
}

func inboundMediaHTTPTransport(cfg InboundMediaConfig) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		host = normalizeInboundMediaHost(host)
		addrs, err := resolveInboundMediaHost(ctx, host)
		if err != nil {
			return nil, err
		}
		if err := validateInboundMediaResolvedAddrs(host, addrs, cfg); err != nil {
			return nil, err
		}
		var lastErr error
		for _, addr := range addrs {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return transport
}

func resolveInboundMediaHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, fmt.Errorf("解析图片地址失败")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("解析图片地址失败")
	}
	for i := range addrs {
		addrs[i] = addrs[i].Unmap()
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("解析图片地址失败")
	}
	return addrs, nil
}

func validateInboundMediaResolvedAddrs(host string, addrs []netip.Addr, cfg InboundMediaConfig) error {
	if cfg.AllowPrivateHosts || inboundMediaAllowedHosts(cfg.AllowedHosts)[normalizeInboundMediaHost(host)] {
		return nil
	}
	for _, addr := range addrs {
		if isPrivateInboundMediaAddr(addr) {
			return fmt.Errorf("图片地址解析到私网或本机地址")
		}
	}
	return nil
}

func validateInboundMediaURL(u *url.URL, cfg InboundMediaConfig) error {
	if u == nil {
		return fmt.Errorf("图片地址无效")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("图片地址只支持 http/https")
	}
	host := normalizeInboundMediaHost(u.Hostname())
	if host == "" {
		return fmt.Errorf("图片地址缺少主机名")
	}
	allowed := inboundMediaAllowedHosts(cfg.AllowedHosts)
	if len(allowed) > 0 {
		if allowed[host] {
			return nil
		}
		return fmt.Errorf("图片来源不在允许列表")
	}
	if !cfg.AllowPrivateHosts && isPrivateInboundMediaHost(host) {
		return fmt.Errorf("默认不接收私网或本机图片地址")
	}
	return nil
}

func inboundMediaAllowedHosts(hosts []string) map[string]bool {
	if len(hosts) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	for _, host := range hosts {
		if normalized := normalizeInboundMediaHost(host); normalized != "" {
			allowed[normalized] = true
		}
	}
	return allowed
}

func normalizeInboundMediaHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	return strings.Trim(host, "[]")
}

func isPrivateInboundMediaHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return isPrivateInboundMediaAddr(addr)
}

func isPrivateInboundMediaAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
}

func inboundMediaFilename(u *url.URL) string {
	if u == nil {
		return ""
	}
	name := path.Base(u.Path)
	if name == "." || name == "/" {
		return ""
	}
	return name
}
