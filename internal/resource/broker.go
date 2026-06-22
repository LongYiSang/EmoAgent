package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

const ProviderLocalFS = "local_fs"

type Broker struct {
	roots            map[string]rootEntry
	refs             map[string]ResourceRef
	maxReadBytes     int64
	maxSearchResults int
	grantStore       GrantStore
}

type rootEntry struct {
	ID        string
	Path      string
	RealPath  string
	Access    string
	Recursive bool
}

type ReadOptions struct {
	MaxBytes int64
}

type ListOptions struct {
	Recursive  bool
	MaxEntries int
	MaxDepth   int
}

type SearchOptions struct {
	Query      string
	MaxResults int
}

type CopyOptions struct {
	MaxBytes int64
}

type Stat struct {
	Ref       ResourceRef `json:"ref"`
	Size      int64       `json:"size"`
	Mode      string      `json:"mode"`
	ModTime   time.Time   `json:"mod_time"`
	IsDir     bool        `json:"is_dir"`
	IsRegular bool        `json:"is_regular"`
}

type DirEntry struct {
	Ref     ResourceRef `json:"ref"`
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Size    int64       `json:"size"`
	ModTime time.Time   `json:"mod_time"`
}

func NewBroker(cfg config.HostResourcesConfig) (*Broker, error) {
	return NewBrokerWithGrantStore(cfg, nil)
}

func NewBrokerWithGrantStore(cfg config.HostResourcesConfig, grantStore GrantStore) (*Broker, error) {
	if cfg.MaxReadBytes <= 0 {
		cfg.MaxReadBytes = 1 << 20
	}
	if cfg.MaxSearchResults <= 0 {
		cfg.MaxSearchResults = 1000
	}
	b := &Broker{
		roots:            make(map[string]rootEntry),
		refs:             make(map[string]ResourceRef),
		maxReadBytes:     cfg.MaxReadBytes,
		maxSearchResults: cfg.MaxSearchResults,
		grantStore:       grantStore,
	}
	rootCatalog, err := buildRootCatalog(cfg)
	if err != nil {
		return nil, err
	}
	for _, root := range rootCatalog {
		entry, err := newRootEntry(root)
		if err != nil {
			return nil, err
		}
		if _, exists := b.roots[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate host resource root %q", entry.ID)
		}
		b.roots[entry.ID] = entry
	}
	return b, nil
}

func NewDenyBroker() *Broker {
	return &Broker{
		roots:            make(map[string]rootEntry),
		refs:             make(map[string]ResourceRef),
		maxReadBytes:     1 << 20,
		maxSearchResults: 1000,
	}
}

func newRootEntry(root config.HostResourceRoot) (rootEntry, error) {
	id := strings.TrimSpace(root.ID)
	if id == "" {
		return rootEntry{}, fmt.Errorf("root id is required")
	}
	rawPath := os.ExpandEnv(strings.TrimSpace(root.Path))
	if rawPath == "" {
		return rootEntry{}, fmt.Errorf("root %q path is required", id)
	}
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return rootEntry{}, fmt.Errorf("resolve root %q: %w", id, err)
	}
	real := evalPathIfExists(abs)
	access := strings.TrimSpace(root.Access)
	switch access {
	case "read", "ask", "deny":
	default:
		return rootEntry{}, fmt.Errorf("root %q access must be read, ask, or deny", id)
	}
	return rootEntry{
		ID:        id,
		Path:      filepath.Clean(abs),
		RealPath:  real,
		Access:    access,
		Recursive: root.Recursive,
	}, nil
}

func (b *Broker) Resolve(_ context.Context, selector ResourceSelector) (ResourceRef, error) {
	resolved, err := b.resolvePath(selector)
	if err != nil {
		return ResourceRef{}, err
	}
	return b.refForResolved(resolved)
}

func (b *Broker) Stat(ctx context.Context, selector ResourceSelector) (Stat, error) {
	ref, err := b.resolveRefForOperation(ctx, selector, OperationMetadata)
	if err != nil {
		return Stat{}, err
	}
	info, err := os.Lstat(ref.CanonicalPath)
	if err != nil {
		return Stat{}, fmt.Errorf("stat resource: %w", err)
	}
	return Stat{
		Ref:       ref,
		Size:      info.Size(),
		Mode:      info.Mode().String(),
		ModTime:   info.ModTime(),
		IsDir:     info.IsDir(),
		IsRegular: info.Mode().IsRegular(),
	}, nil
}

func (b *Broker) Read(ctx context.Context, selector ResourceSelector, opts ReadOptions) ([]byte, ResourceRef, error) {
	ref, err := b.resolveRefForOperation(ctx, selector, OperationRead)
	if err != nil {
		return nil, ResourceRef{}, err
	}
	info, err := os.Lstat(ref.CanonicalPath)
	if err != nil {
		return nil, ResourceRef{}, fmt.Errorf("stat resource: %w", err)
	}
	stat := Stat{Ref: ref, Size: info.Size(), IsDir: info.IsDir()}
	if stat.IsDir {
		return nil, ResourceRef{}, fmt.Errorf("resource is a directory")
	}
	limit := opts.MaxBytes
	if limit <= 0 || limit > b.maxReadBytes {
		limit = b.maxReadBytes
	}
	if stat.Size > limit {
		return nil, ResourceRef{}, fmt.Errorf("resource too large (%d bytes)", stat.Size)
	}
	data, err := os.ReadFile(stat.Ref.CanonicalPath)
	if err != nil {
		return nil, ResourceRef{}, fmt.Errorf("read resource: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, ResourceRef{}, fmt.Errorf("resource too large (%d bytes)", len(data))
	}
	return data, stat.Ref, nil
}

func (b *Broker) List(ctx context.Context, selector ResourceSelector, opts ListOptions) ([]DirEntry, ResourceRef, bool, error) {
	ref, err := b.resolveRefForOperation(ctx, selector, OperationList)
	if err != nil {
		return nil, ResourceRef{}, false, err
	}
	info, err := os.Lstat(ref.CanonicalPath)
	if err != nil {
		return nil, ResourceRef{}, false, fmt.Errorf("stat resource: %w", err)
	}
	if !info.IsDir() {
		return nil, ResourceRef{}, false, fmt.Errorf("resource is not a directory")
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 || maxEntries > b.maxSearchResults {
		maxEntries = b.maxSearchResults
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 32
	}
	recursive := opts.Recursive && b.roots[ref.RootID].Recursive
	var entries []DirEntry
	truncated := false
	addEntry := func(path string, info os.FileInfo) error {
		if protectedPathReason(path) != "" {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(entries) >= maxEntries {
			truncated = true
			return filepath.SkipDir
		}
		if root, ok := b.rootForPath(path); ok && root.Access != "read" {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		childRef, err := b.refForPath(path)
		if err != nil {
			return err
		}
		entries = append(entries, DirEntry{
			Ref:     childRef,
			Name:    info.Name(),
			Type:    resourceType(info),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	}
	if recursive {
		rootDepth := strings.Count(filepath.ToSlash(ref.CanonicalPath), "/")
		err = filepath.WalkDir(ref.CanonicalPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == ref.CanonicalPath {
				return nil
			}
			depth := strings.Count(filepath.ToSlash(path), "/") - rootDepth
			if depth > maxDepth {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			addErr := addEntry(path, info)
			if addErr == filepath.SkipDir && !entry.IsDir() {
				return nil
			}
			return addErr
		})
	} else {
		items, readErr := os.ReadDir(ref.CanonicalPath)
		err = readErr
		for _, item := range items {
			if err != nil {
				break
			}
			info, infoErr := item.Info()
			if infoErr != nil {
				err = infoErr
				break
			}
			err = addEntry(filepath.Join(ref.CanonicalPath, item.Name()), info)
			if err == filepath.SkipDir {
				err = nil
				break
			}
		}
	}
	if err != nil && err != filepath.SkipDir {
		return nil, ResourceRef{}, false, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, ref, truncated, nil
}

func (b *Broker) Search(ctx context.Context, selector ResourceSelector, opts SearchOptions) ([]DirEntry, ResourceRef, bool, error) {
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	if query == "" {
		return nil, ResourceRef{}, false, fmt.Errorf("query is required")
	}
	max := opts.MaxResults
	if max <= 0 || max > b.maxSearchResults {
		max = b.maxSearchResults
	}
	entries, ref, truncated, err := b.List(ctx, selector, ListOptions{Recursive: true, MaxEntries: b.maxSearchResults})
	if err != nil {
		return nil, ResourceRef{}, false, err
	}
	var matches []DirEntry
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name), query) {
			matches = append(matches, entry)
			if len(matches) >= max {
				return matches, ref, true, nil
			}
		}
	}
	return matches, ref, truncated, nil
}

func (b *Broker) CopyToWorkspace(ctx context.Context, selector ResourceSelector, workspaceRoot, targetRel string, opts CopyOptions) (ResourceRef, string, error) {
	ref, err := b.resolveRefForOperation(ctx, selector, OperationCopyToWorkspace)
	if err != nil {
		return ResourceRef{}, "", err
	}
	info, err := os.Lstat(ref.CanonicalPath)
	if err != nil {
		return ResourceRef{}, "", fmt.Errorf("stat resource: %w", err)
	}
	if info.IsDir() {
		return ResourceRef{}, "", fmt.Errorf("resource is a directory")
	}
	limit := opts.MaxBytes
	if limit <= 0 || limit > b.maxReadBytes {
		limit = b.maxReadBytes
	}
	if info.Size() > limit {
		return ResourceRef{}, "", fmt.Errorf("resource too large (%d bytes)", info.Size())
	}
	data, err := os.ReadFile(ref.CanonicalPath)
	if err != nil {
		return ResourceRef{}, "", fmt.Errorf("read resource: %w", err)
	}
	workspaceAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return ResourceRef{}, "", err
	}
	target := filepath.Join(workspaceAbs, targetRel)
	target = filepath.Clean(target)
	workspaceAbs = filepath.Clean(workspaceAbs)
	if !isPathWithin(workspaceAbs, target) {
		return ResourceRef{}, "", fmt.Errorf("target escapes workspace")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return ResourceRef{}, "", err
	}
	if err := ensureWorkspaceWriteTarget(workspaceAbs, target); err != nil {
		return ResourceRef{}, "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return ResourceRef{}, "", err
	}
	return ref, filepath.ToSlash(targetRel), nil
}

func ensureWorkspaceWriteTarget(workspaceRoot, target string) error {
	parent := filepath.Dir(target)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve target parent: %w", err)
	}
	if !isPathWithin(workspaceRoot, filepath.Clean(realParent)) {
		return fmt.Errorf("target parent escapes workspace")
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("target is a symlink")
	}
	return nil
}

type resolvedPath struct {
	root rootEntry
	path string
}

type grantContextKey struct{}

type GrantContext struct {
	ID        string
	Principal PrincipalRef
}

func WithGrant(ctx context.Context, grant GrantContext) context.Context {
	return context.WithValue(ctx, grantContextKey{}, grant)
}

func GrantFromContext(ctx context.Context) (GrantContext, bool) {
	if ctx == nil {
		return GrantContext{}, false
	}
	grant, ok := ctx.Value(grantContextKey{}).(GrantContext)
	return grant, ok
}

func (b *Broker) resolveRefForOperation(ctx context.Context, selector ResourceSelector, operation string) (ResourceRef, error) {
	resolved, err := b.resolvePathForOperation(ctx, selector, operation)
	if err != nil {
		return ResourceRef{}, err
	}
	return b.refForResolved(resolved)
}

func (b *Broker) resolvePath(selector ResourceSelector) (resolvedPath, error) {
	return b.resolvePathForOperation(context.Background(), selector, "")
}

func (b *Broker) resolvePathForOperation(ctx context.Context, selector ResourceSelector, operation string) (resolvedPath, error) {
	if b == nil {
		return resolvedPath{}, fmt.Errorf("host resource broker is unavailable")
	}
	if selector.Kind == ResourceSelectorResourceID {
		if ref, ok := b.refs[selector.ID]; ok {
			return b.resolvePathForOperation(ctx, ResourceSelector{Kind: ResourceSelectorPath, Path: ref.CanonicalPath}, operation)
		}
		return resolvedPath{}, fmt.Errorf("resource_id %q not found", selector.ID)
	}
	path := strings.TrimSpace(selector.Path)
	if path == "" {
		path = strings.TrimSpace(selector.DisplayPath)
	}
	if path == "" && selector.ID != "" {
		path = "@" + selector.ID
	}
	if strings.HasPrefix(path, "@") || selector.Kind == ResourceSelectorAlias {
		return b.resolveAlias(ctx, path, selector.RootID, operation)
	}
	if path == "" {
		return resolvedPath{}, fmt.Errorf("resource path is required")
	}
	if !filepath.IsAbs(path) {
		return resolvedPath{}, fmt.Errorf("host resource path must be an alias, resource_id, or absolute path")
	}
	realPath := evalPathIfExists(path)
	root, ok := b.rootForPath(realPath)
	if ok {
		return b.resolveUnderRoot(ctx, root, realPath, operation)
	}
	return resolvedPath{}, fmt.Errorf("path is outside configured host resource roots")
}

func (b *Broker) resolveAlias(ctx context.Context, path, fallbackRootID, operation string) (resolvedPath, error) {
	aliasPath := strings.TrimPrefix(strings.TrimSpace(path), "@")
	rootID := fallbackRootID
	rest := ""
	if aliasPath != "" {
		parts := strings.SplitN(filepath.ToSlash(aliasPath), "/", 2)
		rootID = parts[0]
		if len(parts) == 2 {
			rest = parts[1]
		}
	}
	root, ok := b.roots[rootID]
	if !ok {
		return resolvedPath{}, fmt.Errorf("host resource root %q is not configured", rootID)
	}
	candidate := root.RealPath
	if rest != "" {
		candidate = filepath.Join(root.RealPath, filepath.FromSlash(rest))
	}
	return b.resolveUnderRoot(ctx, root, evalPathIfExists(candidate), operation)
}

func (b *Broker) resolveUnderRoot(ctx context.Context, root rootEntry, path, operation string) (resolvedPath, error) {
	path = filepath.Clean(path)
	if specific, ok := b.rootForPath(path); ok && specific.RealPath != "" && specific.RealPath != root.RealPath {
		root = specific
	}
	if !isPathWithin(root.RealPath, path) {
		return resolvedPath{}, fmt.Errorf("path escapes host resource root %q", root.ID)
	}
	switch root.Access {
	case "deny":
		return resolvedPath{}, fmt.Errorf("host resource root %q is denied", root.ID)
	case "ask":
		if err := b.authorizeGrant(ctx, root, path, operation); err != nil {
			return resolvedPath{}, err
		}
	}
	if !root.Recursive {
		rel, err := filepath.Rel(root.RealPath, path)
		if err != nil || (rel != "." && strings.Contains(filepath.ToSlash(rel), "/")) {
			return resolvedPath{}, fmt.Errorf("host resource root %q is not recursive", root.ID)
		}
	}
	if protectedPathReason(path) != "" {
		return resolvedPath{}, fmt.Errorf("protected path denied")
	}
	return resolvedPath{root: root, path: path}, nil
}

func (b *Broker) authorizeGrant(ctx context.Context, root rootEntry, path, operation string) error {
	grantCtx, ok := GrantFromContext(ctx)
	if !ok || strings.TrimSpace(grantCtx.ID) == "" {
		return fmt.Errorf("host resource root %q requires grant approval", root.ID)
	}
	if b.grantStore == nil {
		return fmt.Errorf("host resource grant store is unavailable")
	}
	grant, err := b.grantStore.Consume(ctx, grantCtx.ID, grantCtx.Principal)
	if err != nil {
		return fmt.Errorf("host resource grant denied: %w", err)
	}
	if grant.Capability != CapabilityHostFSRead {
		return fmt.Errorf("host resource grant capability mismatch")
	}
	if operation != "" && !containsString(grant.Operations, operation) {
		return fmt.Errorf("host resource grant operation mismatch")
	}
	display := displayPathFor(root, path)
	if !grantMatchesPath(grant, display, root.ID) {
		return fmt.Errorf("host resource grant selector mismatch")
	}
	return nil
}

func grantMatchesPath(grant GrantEnvelope, displayPath, rootID string) bool {
	candidates := []string{
		strings.TrimSpace(grant.Resource.Path),
		strings.TrimSpace(grant.Resource.DisplayPath),
	}
	if grant.Resource.Kind == ResourceSelectorAlias && grant.Resource.ID != "" {
		candidates = append(candidates, "@"+grant.Resource.ID)
	}
	if grant.Resource.RootID != "" {
		candidates = append(candidates, "@"+grant.Resource.RootID)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if candidate == displayPath {
			return true
		}
		if grant.Constraints.Recursive && strings.HasPrefix(displayPath+"/", strings.TrimRight(candidate, "/")+"/") {
			return true
		}
	}
	return rootID != "" && grant.Constraints.Recursive && grant.Resource.RootID == rootID
}

func displayPathFor(root rootEntry, path string) string {
	rel, err := filepath.Rel(root.RealPath, path)
	if err != nil || rel == "." {
		return "@" + root.ID
	}
	return "@" + root.ID + "/" + filepath.ToSlash(rel)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (b *Broker) refForResolved(resolved resolvedPath) (ResourceRef, error) {
	return b.refForPathUnderRoot(resolved.root, resolved.path)
}

func (b *Broker) refForPath(path string) (ResourceRef, error) {
	root, ok := b.rootForPath(path)
	if !ok {
		return ResourceRef{}, fmt.Errorf("path is outside configured host resource roots")
	}
	if root.Access != "read" {
		return ResourceRef{}, fmt.Errorf("host resource root %q requires explicit resolution", root.ID)
	}
	return b.refForPathUnderRoot(root, path)
}

func (b *Broker) refForPathUnderRoot(root rootEntry, path string) (ResourceRef, error) {
	if protectedPathReason(path) != "" {
		return ResourceRef{}, fmt.Errorf("protected path denied")
	}
	rel, err := filepath.Rel(root.RealPath, path)
	if err != nil {
		return ResourceRef{}, err
	}
	display := "@" + root.ID
	if rel != "." {
		display += "/" + filepath.ToSlash(rel)
	}
	info, _ := os.Lstat(path)
	ref := ResourceRef{
		ID:                "local:" + shortHash(path),
		Provider:          ProviderLocalFS,
		DisplayPath:       display,
		RootID:            root.ID,
		CanonicalPath:     path,
		CanonicalPathHash: "sha256:" + sha256Hex([]byte(path)),
		ResourceType:      resourceType(info),
		FileIdentity:      fileIdentity(path, info),
	}
	b.refs[ref.ID] = ref
	return ref, nil
}

func (b *Broker) rootForPath(path string) (rootEntry, bool) {
	var picked rootEntry
	var found bool
	for _, root := range b.roots {
		if !isPathWithin(root.RealPath, path) {
			continue
		}
		if !found || len(root.RealPath) > len(picked.RealPath) {
			picked = root
			found = true
		}
	}
	return picked, found
}

func evalPathIfExists(path string) string {
	clean := filepath.Clean(path)
	realPath, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return filepath.Clean(realPath)
	}
	var suffix []string
	current := clean
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return clean
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
		realParent, err := filepath.EvalSymlinks(current)
		if err == nil {
			parts := append([]string{realParent}, suffix...)
			return filepath.Clean(filepath.Join(parts...))
		}
	}
}

func isPathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(filepath.ToSlash(rel), "../"))
}

func protectedPathReason(path string) string {
	lower := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	for _, segment := range strings.Split(lower, "/") {
		switch segment {
		case ".ssh", ".gnupg", ".aws", ".azure", ".gcloud", ".kube", ".password-store", ".git":
			return "credential"
		}
	}
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") ||
		strings.HasSuffix(base, ".pem") ||
		strings.HasSuffix(base, ".key") ||
		strings.HasSuffix(base, ".p12") ||
		strings.HasSuffix(base, ".pfx") ||
		base == "emo.db" ||
		strings.Contains(base, "trivium") ||
		strings.HasPrefix(base, "credential") ||
		strings.HasPrefix(base, "secret") ||
		strings.HasPrefix(base, "token") {
		return "protected"
	}
	if strings.Contains(lower, "/raw_log_extraction/") || strings.Contains(lower, "/memorycore/") {
		return "memory_authority"
	}
	return ""
}

func resourceType(info os.FileInfo) string {
	if info == nil {
		return "other"
	}
	mode := info.Mode()
	switch {
	case mode.IsDir():
		return "directory"
	case mode.IsRegular():
		return "file"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	default:
		return "other"
	}
}

func fileIdentity(path string, info os.FileInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(path), info.Size(), info.ModTime().UnixNano())
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
