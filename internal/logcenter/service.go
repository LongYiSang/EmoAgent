package logcenter

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultCapacity         = 5000
	DefaultSubscriberBuffer = 256
	DefaultPollInterval     = time.Second
)

type SourceType string

const (
	SourceTypeMain    SourceType = "main"
	SourceTypeSidecar SourceType = "sidecar"
	SourceTypePlugin  SourceType = "plugin"
)

type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarn    Level = "warn"
	LevelError   Level = "error"
	LevelUnknown Level = "unknown"
)

type SourceStatus string

const (
	SourceStatusActive      SourceStatus = "active"
	SourceStatusDegraded    SourceStatus = "degraded"
	SourceStatusUnavailable SourceStatus = "unavailable"
)

type Event struct {
	ID          string            `json:"id"`
	Time        time.Time         `json:"time"`
	SourceType  SourceType        `json:"source_type"`
	SourceID    string            `json:"source_id"`
	SourceLabel string            `json:"source_label,omitempty"`
	Level       Level             `json:"level"`
	Message     string            `json:"message"`
	Fingerprint string            `json:"fingerprint"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Raw         string            `json:"raw,omitempty"`
}

type Source struct {
	ID            string       `json:"id"`
	Type          SourceType   `json:"type"`
	Label         string       `json:"label"`
	Status        SourceStatus `json:"status"`
	LastEventTime *time.Time   `json:"last_event_time,omitempty"`
	LastError     string       `json:"last_error,omitempty"`
}

type Query struct {
	Limit      int
	SourceType SourceType
	SourceID   string
	MinLevel   Level
	AfterID    string
}

type SourceTail struct {
	Source Source
	Tail   string
}

type SourceProvider interface {
	LogCenterSources(context.Context) []SourceTail
}

type Service struct {
	mu          sync.Mutex
	events      []Event
	sources     map[string]Source
	subscribers map[chan Event]struct{}
	cursors     map[string]string
	providers   []SourceProvider
	capacity    int
	subBuffer   int
	pollEvery   time.Duration
	nextID      uint64
	cancel      context.CancelFunc
	done        chan struct{}
}

func NewService() *Service {
	s := &Service{
		capacity:    DefaultCapacity,
		subBuffer:   DefaultSubscriberBuffer,
		pollEvery:   DefaultPollInterval,
		sources:     map[string]Source{},
		subscribers: map[chan Event]struct{}{},
		cursors:     map[string]string{},
	}
	s.sources[sourceKey(SourceTypeMain, "host")] = Source{ID: "host", Type: SourceTypeMain, Label: "EmoAgent 主程序", Status: SourceStatusActive}
	return s
}

func (s *Service) SetProviders(providers ...SourceProvider) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = append([]SourceProvider(nil), providers...)
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go s.run(runCtx, done)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	for ch := range s.subscribers {
		delete(s.subscribers, ch)
		close(ch)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	s.Poll(ctx)
	ticker := time.NewTicker(s.pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Poll(ctx)
		}
	}
}

func (s *Service) Poll(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	providers := append([]SourceProvider(nil), s.providers...)
	s.mu.Unlock()

	for _, provider := range providers {
		for _, item := range provider.LogCenterSources(ctx) {
			s.updateSource(item.Source)
			s.ingestTail(item.Source, item.Tail)
		}
	}
}

func (s *Service) Add(event Event) Event {
	if s == nil {
		return event
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(event)
}

func (s *Service) addLocked(event Event) Event {
	s.nextID++
	event.ID = strconv.FormatUint(s.nextID, 10)
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if event.Level == "" {
		event.Level = LevelUnknown
	}
	if event.SourceType == "" {
		event.SourceType = SourceTypeMain
	}
	if event.SourceID == "" {
		event.SourceID = "host"
	}
	event.Message = maskSensitiveText(event.Message)
	event.Raw = maskSensitiveText(event.Raw)
	event.Attrs = maskSensitiveAttrs(event.Attrs)
	if event.Fingerprint == "" {
		event.Fingerprint = fingerprint(event)
	}
	s.events = append(s.events, event)
	if len(s.events) > s.capacity {
		s.events = append([]Event(nil), s.events[len(s.events)-s.capacity:]...)
	}
	key := sourceKey(event.SourceType, event.SourceID)
	source := s.sources[key]
	source.ID = event.SourceID
	source.Type = event.SourceType
	if event.SourceLabel != "" {
		source.Label = event.SourceLabel
	}
	if source.Status == "" {
		source.Status = SourceStatusActive
	}
	lastEventTime := event.Time
	source.LastEventTime = &lastEventTime
	s.sources[key] = source
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			delete(s.subscribers, ch)
			close(ch)
		}
	}
	return event
}

func (s *Service) Events(q Query) []Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.events
	if q.AfterID != "" {
		for i, event := range events {
			if event.ID == q.AfterID {
				events = events[i+1:]
				break
			}
		}
	}
	out := make([]Event, 0, len(events))
	for _, event := range events {
		if !Match(event, q) {
			continue
		}
		out = append(out, event)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return append([]Event(nil), out...)
}

func (s *Service) Sources() []Source {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Source, 0, len(s.sources))
	for _, source := range s.sources {
		out = append(out, source)
	}
	return out
}

func (s *Service) Subscribe() (<-chan Event, func()) {
	if s == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, s.subBuffer)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

func (s *Service) Handler(source Source) slog.Handler {
	return slogHandler{service: s, source: source}
}

func (s *Service) updateSource(source Source) {
	if source.ID == "" || source.Type == "" {
		return
	}
	if source.Status == "" {
		source.Status = SourceStatusActive
	}
	s.mu.Lock()
	existing := s.sources[sourceKey(source.Type, source.ID)]
	if source.LastEventTime == nil {
		source.LastEventTime = existing.LastEventTime
	}
	s.sources[sourceKey(source.Type, source.ID)] = source
	s.mu.Unlock()
}

func (s *Service) ingestTail(source Source, tail string) {
	if strings.TrimSpace(tail) == "" || source.ID == "" || source.Type == "" {
		return
	}
	key := sourceKey(source.Type, source.ID)
	s.mu.Lock()
	previous := s.cursors[key]
	if previous == tail {
		s.mu.Unlock()
		return
	}
	s.cursors[key] = tail
	s.mu.Unlock()

	next := tail
	if previous != "" && strings.HasPrefix(tail, previous) {
		next = strings.TrimPrefix(tail, previous)
	}
	for _, line := range strings.Split(next, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		level, message := parseTailLine(line)
		s.Add(Event{
			Time:        time.Now(),
			SourceType:  source.Type,
			SourceID:    source.ID,
			SourceLabel: source.Label,
			Level:       level,
			Message:     message,
			Raw:         line,
		})
	}
}

type slogHandler struct {
	service *Service
	source  Source
	attrs   []slog.Attr
}

func (h slogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h slogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]string{}
	for _, attr := range h.attrs {
		addAttr(attrs, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(attrs, attr)
		return true
	})
	h.service.Add(Event{
		Time:        record.Time,
		SourceType:  h.source.Type,
		SourceID:    h.source.ID,
		SourceLabel: h.source.Label,
		Level:       levelFromSlog(record.Level),
		Message:     record.Message,
		Attrs:       attrs,
	})
	return nil
}

func (h slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return next
}

func (h slogHandler) WithGroup(string) slog.Handler {
	return h
}

func addAttr(attrs map[string]string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Key == "" {
		return
	}
	attrs[attr.Key] = attr.Value.String()
}

func Match(event Event, q Query) bool {
	if q.SourceType != "" && event.SourceType != q.SourceType {
		return false
	}
	if q.SourceID != "" && event.SourceID != q.SourceID {
		return false
	}
	if q.MinLevel != "" {
		return levelRank(event.Level) >= levelRank(q.MinLevel)
	}
	return true
}

func levelRank(level Level) int {
	switch level {
	case LevelDebug:
		return 0
	case LevelInfo:
		return 1
	case LevelWarn:
		return 2
	case LevelError:
		return 3
	default:
		return -1
	}
}

func levelFromSlog(level slog.Level) Level {
	switch {
	case level >= slog.LevelError:
		return LevelError
	case level >= slog.LevelWarn:
		return LevelWarn
	case level <= slog.LevelDebug:
		return LevelDebug
	default:
		return LevelInfo
	}
}

var bracketLevelRE = regexp.MustCompile(`^\[(debug|info|warn|warning|error)\]\s*(.*)$`)
var tokenLevelRE = regexp.MustCompile(`(?i)\b(error|warn|warning|info|debug)\b`)
var sensitiveTextRE = regexp.MustCompile(`(?i)("?(?:api_key|authorization|token|password)"?\s*[:=]\s*)"?[^"\s,;}]+`)

func parseTailLine(line string) (Level, string) {
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, "{") {
		var payload map[string]any
		if json.Unmarshal([]byte(text), &payload) == nil {
			level := parseLevelString(stringValue(payload["level"]))
			msg := firstNonEmpty(stringValue(payload["message"]), stringValue(payload["msg"]), text)
			return level, msg
		}
	}
	if matches := bracketLevelRE.FindStringSubmatch(text); len(matches) == 3 {
		return parseLevelString(matches[1]), matches[2]
	}
	if match := tokenLevelRE.FindString(text); match != "" {
		return parseLevelString(match), text
	}
	return LevelUnknown, text
}

func parseLevelString(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelUnknown
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func fingerprint(event Event) string {
	h := fnv.New64a()
	h.Write([]byte(event.SourceType))
	h.Write([]byte{0})
	h.Write([]byte(event.SourceID))
	h.Write([]byte{0})
	h.Write([]byte(event.Level))
	h.Write([]byte{0})
	h.Write([]byte(event.Message))
	h.Write([]byte{0})
	h.Write([]byte(event.Raw))
	return strconv.FormatUint(h.Sum64(), 16)
}

func sourceKey(sourceType SourceType, sourceID string) string {
	return string(sourceType) + "/" + sourceID
}

func maskSensitiveAttrs(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return attrs
	}
	next := make(map[string]string, len(attrs))
	for key, value := range attrs {
		if isSensitiveKey(key) {
			next[key] = "[redacted]"
			continue
		}
		next[key] = maskSensitiveText(value)
	}
	return next
}

func maskSensitiveText(text string) string {
	if text == "" {
		return text
	}
	return sensitiveTextRE.ReplaceAllString(text, "${1}[redacted]")
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "api_key") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "password")
}
