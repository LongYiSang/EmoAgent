package onebotv11

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/platform"
)

func TestResolveInboundMediaTextAndImage(t *testing.T) {
	server := testImageServer(t)
	store := &fakeInboundMediaStore{}
	cfg := testInboundMediaMessageConfig()

	parts, text, err := resolveInboundMedia(context.Background(), RawMessageValue{Segments: Message{
		{Type: "text", Data: map[string]string{"text": "look"}},
		{Type: "image", Data: map[string]string{"url": server.URL + "/tiny.png"}},
	}}, cfg, store)
	if err != nil {
		t.Fatalf("resolveInboundMedia: %v", err)
	}
	if text != "look\n[used image]" {
		t.Fatalf("text = %q, want rendered placeholder", text)
	}
	if len(parts) != 2 || parts[0].Type != string(llm.PartText) || parts[0].Text != "look" || parts[1].Media == nil || parts[1].Media.MediaAssetID != "med_1" {
		t.Fatalf("parts = %#v, want text plus image media part", parts)
	}
	if !bytes.Equal(store.lastUpload, tinyOneBotPNG()) {
		t.Fatalf("uploaded bytes len = %d, want tiny PNG", len(store.lastUpload))
	}
}

func TestResolveInboundMediaPureImage(t *testing.T) {
	server := testImageServer(t)
	store := &fakeInboundMediaStore{}
	cfg := testInboundMediaMessageConfig()

	parts, text, err := resolveInboundMedia(context.Background(), RawMessageValue{Segments: Message{
		{Type: "image", Data: map[string]string{"url": server.URL + "/tiny.png"}},
	}}, cfg, store)
	if err != nil {
		t.Fatalf("resolveInboundMedia: %v", err)
	}
	if text != "[used image]" {
		t.Fatalf("text = %q, want image placeholder", text)
	}
	if len(parts) != 1 || parts[0].Media == nil || parts[0].Media.Kind != "image" {
		t.Fatalf("parts = %#v, want one image part", parts)
	}
}

func TestResolveInboundMediaRejectsTooManyImages(t *testing.T) {
	store := &fakeInboundMediaStore{}
	cfg := testInboundMediaMessageConfig()
	cfg.InboundMedia.MaxImages = 1

	_, _, err := resolveInboundMedia(context.Background(), RawMessageValue{Segments: Message{
		{Type: "image", Data: map[string]string{"url": "https://example.com/a.png"}},
		{Type: "image", Data: map[string]string{"url": "https://example.com/b.png"}},
	}}, cfg, store)
	if err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatalf("resolveInboundMedia error = %v, want max image error", err)
	}
	if store.uploads != 0 {
		t.Fatalf("uploads = %d, want no download after max image rejection", store.uploads)
	}
}

func TestResolveInboundMediaIgnoresQQFaceSegments(t *testing.T) {
	store := &fakeInboundMediaStore{}
	cfg := testInboundMediaMessageConfig()

	parts, text, err := resolveInboundMedia(context.Background(), RawMessageValue{Segments: Message{
		{Type: "text", Data: map[string]string{"text": "hi"}},
		{Type: "face", Data: map[string]string{"id": "123"}},
		{Type: "mface", Data: map[string]string{"id": "456"}},
	}}, cfg, store)
	if err != nil {
		t.Fatalf("resolveInboundMedia: %v", err)
	}
	if len(parts) != 0 || text != "" || store.uploads != 0 {
		t.Fatalf("parts=%#v text=%q uploads=%d, want no image handling", parts, text, store.uploads)
	}
}

func TestResolveInboundMediaRejectsPrivateHostsByDefault(t *testing.T) {
	server := testImageServer(t)
	store := &fakeInboundMediaStore{}
	cfg := testInboundMediaMessageConfig()
	cfg.InboundMedia.AllowPrivateHosts = false
	cfg.InboundMedia.AllowedHosts = nil

	_, _, err := resolveInboundMedia(context.Background(), RawMessageValue{Segments: Message{
		{Type: "image", Data: map[string]string{"url": server.URL + "/tiny.png"}},
	}}, cfg, store)
	if err == nil || !strings.Contains(err.Error(), "私网") {
		t.Fatalf("resolveInboundMedia error = %v, want private host rejection", err)
	}
	if store.uploads != 0 {
		t.Fatalf("uploads = %d, want no upload", store.uploads)
	}
}

func TestValidateInboundMediaResolvedAddrsRejectsPrivateDNSByDefault(t *testing.T) {
	cfg := testInboundMediaMessageConfig()
	cfg.InboundMedia.AllowPrivateHosts = false
	cfg.InboundMedia.AllowedHosts = nil

	err := validateInboundMediaResolvedAddrs("example.test", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, cfg.InboundMedia)
	if err == nil || !strings.Contains(err.Error(), "私网") {
		t.Fatalf("validate error = %v, want private DNS rejection", err)
	}
}

func TestValidateInboundMediaResolvedAddrsAllowsWhitelistedPrivateDNS(t *testing.T) {
	cfg := testInboundMediaMessageConfig()
	cfg.InboundMedia.AllowPrivateHosts = false
	cfg.InboundMedia.AllowedHosts = []string{"example.test"}

	err := validateInboundMediaResolvedAddrs("example.test", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, cfg.InboundMedia)
	if err != nil {
		t.Fatalf("validate error = %v, want whitelisted host allowed", err)
	}
}

func TestAdapterResolvesInboundPrivateImageBeforeGateway(t *testing.T) {
	server := testImageServer(t)
	cfg := testConfig()
	cfg.Message.InboundMedia = testInboundMediaMessageConfig().InboundMedia
	transport := &mediaAdapterTransport{recordingActionClient: &recordingActionClient{}}
	adapter, err := NewAdapterWithTransport("qq-main", cfg, transport)
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}
	adapter.SetInboundMediaStore(&fakeInboundMediaStore{})
	handler := &recordingInboundHandler{}
	if err := adapter.Start(context.Background(), handler); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result, err := adapter.HandleEvent(context.Background(), onebotImageEvent(Message{
		{Type: "text", Data: map[string]string{"text": "look"}},
		{Type: "image", Data: map[string]string{"url": server.URL + "/tiny.png"}},
	}))
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if !result.Handled || handler.calls != 1 {
		t.Fatalf("result=%#v handler calls=%d, want gateway call", result, handler.calls)
	}
	if handler.inbound.Text != "look\n[used image]" || len(handler.inbound.Parts) != 2 || handler.inbound.Parts[1].Media == nil {
		t.Fatalf("inbound = %#v, want resolved image parts", handler.inbound)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("transport requests = %#v, want no failure notice", transport.requests)
	}
}

func TestAdapterNotifiesPrivateImageFailureWithoutGatewayCall(t *testing.T) {
	cfg := testConfig()
	cfg.Message.InboundMedia = testInboundMediaMessageConfig().InboundMedia
	cfg.Message.InboundMedia.MaxImages = 1
	transport := &mediaAdapterTransport{recordingActionClient: &recordingActionClient{}}
	adapter, err := NewAdapterWithTransport("qq-main", cfg, transport)
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}
	adapter.SetInboundMediaStore(&fakeInboundMediaStore{})
	handler := &recordingInboundHandler{}
	if err := adapter.Start(context.Background(), handler); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result, err := adapter.HandleEvent(context.Background(), onebotImageEvent(Message{
		{Type: "image", Data: map[string]string{"url": "https://example.com/a.png"}},
		{Type: "image", Data: map[string]string{"url": "https://example.com/b.png"}},
	}))
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if !result.Ignored || handler.calls != 0 {
		t.Fatalf("result=%#v handler calls=%d, want ignored before gateway", result, handler.calls)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("transport requests = %#v, want one failure notice", transport.requests)
	}
	req := transport.requests[0]
	if req.Action != "send_private_msg" || req.Params["user_id"] != int64(11223344) {
		t.Fatalf("request = %#v, want private failure notice", req)
	}
	segments, ok := req.Params["message"].([]Segment)
	if !ok || len(segments) != 1 || !strings.Contains(segments[0].Data["text"], "图片接收失败") {
		t.Fatalf("message param = %#v, want failure text", req.Params["message"])
	}
}

func testInboundMediaMessageConfig() MessageConfig {
	return MessageConfig{
		UnsupportedSegmentPolicy: UnsupportedSegmentPlaceholder,
		InboundMedia: InboundMediaConfig{
			Enabled:           true,
			MaxImages:         4,
			DownloadTimeoutMS: 1000,
			AllowPrivateHosts: true,
			AllowedHosts:      []string{"127.0.0.1"},
			OnFailure:         "notify",
		},
	}
}

func onebotImageEvent(message Message) Event {
	return Event{
		Time:        1710000000,
		SelfID:      123456,
		PostType:    "message",
		MessageType: "private",
		MessageID:   []byte("24680"),
		UserID:      11223344,
		Message:     RawMessageValue{Segments: message},
		Sender:      Sender{UserID: 11223344, Nickname: "Alice"},
	}
}

func testImageServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyOneBotPNG())
	}))
	t.Cleanup(server.Close)
	return server
}

type fakeInboundMediaStore struct {
	uploads    int
	lastUpload []byte
}

func (s *fakeInboundMediaStore) Upload(_ context.Context, r io.Reader, meta media.UploadMeta) (*media.MediaAsset, error) {
	s.uploads++
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	s.lastUpload = data
	filename := meta.OriginalFilename
	if filename == "" {
		filename = "tiny.png"
	}
	return &media.MediaAsset{
		ID:               "med_1",
		Kind:             "image",
		MimeType:         "image/png",
		OriginalFilename: filename,
		ByteSize:         int64(len(data)),
	}, nil
}

func tinyOneBotPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
}

type mediaAdapterTransport struct {
	*recordingActionClient
}

func (t *mediaAdapterTransport) Start(context.Context, EventHandler) error {
	return nil
}

func (t *mediaAdapterTransport) Stop(context.Context) error {
	return nil
}

func (t *mediaAdapterTransport) Status() TransportStatus {
	return TransportStatus{}
}

type recordingInboundHandler struct {
	calls   int
	inbound platform.InboundMessage
}

func (h *recordingInboundHandler) HandleInbound(_ context.Context, in platform.InboundMessage, _ platform.OutboundSink) (platform.HandleResult, error) {
	h.calls++
	h.inbound = in
	return platform.HandleResult{Handled: true, SessionID: "session-1"}, nil
}
