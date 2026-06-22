package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ChangeSetManagerOptions struct {
	StagingDir    string
	QuarantineDir string
	MaxBytes      int64
}

type ChangeSetManager struct {
	mu            sync.Mutex
	broker        *Broker
	store         ChangeSetStore
	stagingDir    string
	quarantineDir string
	maxBytes      int64
	memory        map[string]ChangeSet
}

func NewChangeSetManager(broker *Broker, store ChangeSetStore, opts ChangeSetManagerOptions) (*ChangeSetManager, error) {
	if broker == nil {
		return nil, fmt.Errorf("host resource broker is unavailable")
	}
	stagingDir := strings.TrimSpace(opts.StagingDir)
	if stagingDir == "" {
		stagingDir = filepath.Join("data", "resource-staging")
	}
	quarantineDir := strings.TrimSpace(opts.QuarantineDir)
	if quarantineDir == "" {
		quarantineDir = filepath.Join("data", "resource-quarantine")
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	return &ChangeSetManager{
		broker:        broker,
		store:         store,
		stagingDir:    filepath.Clean(stagingDir),
		quarantineDir: filepath.Clean(quarantineDir),
		maxBytes:      maxBytes,
		memory:        map[string]ChangeSet{},
	}, nil
}

func (m *ChangeSetManager) StageResource(_ context.Context, content string) (StagedResource, error) {
	if m == nil {
		return StagedResource{}, fmt.Errorf("host resource changeset manager is unavailable")
	}
	data := []byte(content)
	if int64(len(data)) > m.maxBytes {
		return StagedResource{}, fmt.Errorf("staged resource too large (%d bytes)", len(data))
	}
	if err := os.MkdirAll(m.stagingBlobDir(), 0o700); err != nil {
		return StagedResource{}, fmt.Errorf("create staging dir: %w", err)
	}
	staged := StagedResource{
		ID:          "stage-" + uuid.NewString(),
		ContentHash: "sha256:" + sha256Hex(data),
		Bytes:       int64(len(data)),
		CreatedAt:   time.Now().UTC(),
	}
	staged.Path = filepath.Join(m.stagingBlobDir(), staged.ID+".blob")
	if err := os.WriteFile(staged.Path, data, 0o600); err != nil {
		return StagedResource{}, fmt.Errorf("write staged resource: %w", err)
	}
	return staged, nil
}

func (m *ChangeSetManager) PrepareChange(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	if m == nil {
		return ChangeSet{}, fmt.Errorf("host resource changeset manager is unavailable")
	}
	if req.PermanentDelete && req.Operation != ChangeOpDelete {
		return ChangeSet{}, fmt.Errorf("permanent delete is only supported for delete changesets")
	}
	if req.Recursive && req.Operation != ChangeOpDelete {
		return ChangeSet{}, fmt.Errorf("recursive external directory changes are only supported for delete changesets")
	}
	var cs ChangeSet
	var err error
	switch req.Operation {
	case ChangeOpCreateFile:
		cs, err = m.prepareCreateFile(ctx, req)
	case ChangeOpOverwriteFile:
		cs, err = m.prepareOverwriteFile(ctx, req)
	case ChangeOpMove:
		cs, err = m.prepareMove(ctx, req)
	case ChangeOpDelete:
		cs, err = m.prepareDelete(ctx, req)
	case ChangeOpMkdir:
		cs, err = m.prepareMkdir(ctx, req)
	case ChangeOpRmdir:
		cs, err = m.prepareRmdir(ctx, req)
	default:
		err = fmt.Errorf("unsupported change operation %q", req.Operation)
	}
	if err != nil {
		return ChangeSet{}, err
	}
	if err := m.save(ctx, cs); err != nil {
		return ChangeSet{}, err
	}
	return cs, nil
}

func (m *ChangeSetManager) PreviewChange(ctx context.Context, id string) (ChangeSet, error) {
	cs, ok, err := m.get(ctx, id)
	if err != nil {
		return ChangeSet{}, err
	}
	if !ok {
		return ChangeSet{}, fmt.Errorf("changeset %q not found", id)
	}
	if cs.Preview.Diff == "" && cs.StagingPath != "" && (cs.Operation == ChangeOpOverwriteFile || cs.Operation == ChangeOpCreateFile) {
		if diff, binary := m.diffForChangeSet(cs); diff != "" || binary {
			cs.Preview.Diff = diff
			cs.Preview.Binary = binary
		}
	}
	return cs, nil
}

func (m *ChangeSetManager) ApplyChange(ctx context.Context, id, planHash string) (ChangeSet, error) {
	return m.ApplyChangeWithOptions(ctx, id, planHash, ChangeApplyOptions{})
}

func (m *ChangeSetManager) ApplyChangeWithOptions(ctx context.Context, id, planHash string, opts ChangeApplyOptions) (ChangeSet, error) {
	cs, ok, err := m.get(ctx, id)
	if err != nil {
		return ChangeSet{}, err
	}
	if !ok {
		return ChangeSet{}, fmt.Errorf("changeset %q not found", id)
	}
	if cs.Status != ChangeSetStatusApprovalPending {
		return ChangeSet{}, fmt.Errorf("changeset %q is not approval_pending", id)
	}
	if strings.TrimSpace(planHash) == "" || planHash != cs.PlanHash {
		return m.markConflict(ctx, cs, "plan hash mismatch")
	}
	if err := validateApplyOptions(cs, opts); err != nil {
		return ChangeSet{}, err
	}
	cs.Status = ChangeSetStatusApplying
	cs.UpdatedAt = time.Now().UTC()
	_ = m.update(ctx, cs)

	switch cs.Operation {
	case ChangeOpCreateFile:
		cs, err = m.applyCreateFile(cs)
	case ChangeOpOverwriteFile:
		cs, err = m.applyOverwriteFile(cs)
	case ChangeOpMove:
		cs, err = m.applyMove(cs)
	case ChangeOpDelete:
		cs, err = m.applyDelete(cs)
	case ChangeOpMkdir:
		cs, err = m.applyMkdir(cs)
	case ChangeOpRmdir:
		cs, err = m.applyRmdir(cs)
	default:
		err = fmt.Errorf("unsupported change operation %q", cs.Operation)
	}
	if err != nil {
		if isConflictError(err) {
			return m.markConflict(ctx, cs, strings.TrimPrefix(err.Error(), "conflict: "))
		}
		cs.Status = ChangeSetStatusFailed
		cs.ErrorMessage = err.Error()
		cs.UpdatedAt = time.Now().UTC()
		_ = m.update(ctx, cs)
		return cs, err
	}
	now := time.Now().UTC()
	cs.Status = ChangeSetStatusApplied
	cs.AppliedAt = &now
	cs.UpdatedAt = now
	cs.ErrorMessage = ""
	if err := m.update(ctx, cs); err != nil {
		return ChangeSet{}, err
	}
	return cs, nil
}

func (m *ChangeSetManager) CancelChange(ctx context.Context, id string) (ChangeSet, error) {
	cs, ok, err := m.get(ctx, id)
	if err != nil {
		return ChangeSet{}, err
	}
	if !ok {
		return ChangeSet{}, fmt.Errorf("changeset %q not found", id)
	}
	if cs.Status == ChangeSetStatusApplied || cs.Status == ChangeSetStatusRestored {
		return ChangeSet{}, fmt.Errorf("changeset %q is already finalized", id)
	}
	cs.Status = ChangeSetStatusCancelled
	cs.UpdatedAt = time.Now().UTC()
	if err := m.update(ctx, cs); err != nil {
		return ChangeSet{}, err
	}
	return cs, nil
}

func (m *ChangeSetManager) RestoreQuarantine(ctx context.Context, id, planHash string) (ChangeSet, error) {
	cs, ok, err := m.get(ctx, id)
	if err != nil {
		return ChangeSet{}, err
	}
	if !ok {
		return ChangeSet{}, fmt.Errorf("changeset %q not found", id)
	}
	if cs.Status != ChangeSetStatusApplied || cs.QuarantinePath == "" {
		return ChangeSet{}, fmt.Errorf("changeset %q has no restorable quarantine", id)
	}
	if strings.TrimSpace(planHash) == "" || planHash != cs.PlanHash {
		return m.markConflict(ctx, cs, "plan hash mismatch")
	}
	if _, err := os.Lstat(cs.Source.CanonicalPath); err == nil {
		return m.markConflict(ctx, cs, "restore target already exists")
	}
	if err := m.ensureParentSafe(cs.Source); err != nil {
		return m.markConflict(ctx, cs, err.Error())
	}
	if err := os.Rename(cs.QuarantinePath, cs.Source.CanonicalPath); err != nil {
		return ChangeSet{}, fmt.Errorf("restore quarantine: %w", err)
	}
	cs.Status = ChangeSetStatusRestored
	cs.UpdatedAt = time.Now().UTC()
	if err := m.update(ctx, cs); err != nil {
		return ChangeSet{}, err
	}
	return cs, nil
}

func (m *ChangeSetManager) prepareCreateFile(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	target, err := m.resolveTarget(ctx, req, OperationCreate)
	if err != nil {
		return ChangeSet{}, err
	}
	if err := m.ensureTargetAbsent(target); err != nil {
		return ChangeSet{}, err
	}
	if err := m.ensureParentSafe(target); err != nil {
		return ChangeSet{}, err
	}
	staged, err := m.stageForRequest(ctx, req)
	if err != nil {
		return ChangeSet{}, err
	}
	cs := m.newChangeSet(req, ChangeOpCreateFile)
	cs.Target = target
	cs.TargetDisplayPath = target.DisplayPath
	cs.StagingPath = staged.Path
	cs.ContentHash = staged.ContentHash
	cs.Preview = ChangePreview{
		Summary:       "create " + target.DisplayPath,
		Bytes:         staged.Bytes,
		AffectedFiles: 1,
		Ops: []ChangeOp{{
			Index:       0,
			Operation:   ChangeOpCreateFile,
			TargetPath:  target.DisplayPath,
			TargetHash:  staged.ContentHash,
			Bytes:       staged.Bytes,
			Description: "create file",
		}},
	}
	if diff, binary := m.diffBytes(nil, mustReadFile(staged.Path)); diff != "" || binary {
		cs.Preview.Diff = diff
		cs.Preview.Binary = binary
	}
	cs.PlanHash = planHash(cs)
	return cs, nil
}

func (m *ChangeSetManager) prepareOverwriteFile(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	source, err := m.resolveSource(ctx, req, OperationOverwrite)
	if err != nil {
		return ChangeSet{}, err
	}
	baseline, err := m.existingFileSnapshot(source)
	if err != nil {
		return ChangeSet{}, err
	}
	staged, err := m.stageForRequest(ctx, req)
	if err != nil {
		return ChangeSet{}, err
	}
	cs := m.newChangeSet(req, ChangeOpOverwriteFile)
	cs.Source = source
	cs.Target = source
	cs.TargetDisplayPath = source.DisplayPath
	cs.BaselineHash = baseline.hash
	cs.BaselineFileID = baseline.fileID
	cs.StagingPath = staged.Path
	cs.ContentHash = staged.ContentHash
	cs.Preview = ChangePreview{
		Summary:       "overwrite " + source.DisplayPath,
		Bytes:         staged.Bytes,
		AffectedFiles: 1,
		Ops: []ChangeOp{{
			Index:       0,
			Operation:   ChangeOpOverwriteFile,
			SourcePath:  source.DisplayPath,
			TargetPath:  source.DisplayPath,
			SourceHash:  baseline.hash,
			TargetHash:  staged.ContentHash,
			Bytes:       staged.Bytes,
			Description: "overwrite file",
		}},
	}
	if diff, binary := m.diffBytes(baseline.data, mustReadFile(staged.Path)); diff != "" || binary {
		cs.Preview.Diff = diff
		cs.Preview.Binary = binary
	}
	cs.PlanHash = planHash(cs)
	return cs, nil
}

func (m *ChangeSetManager) prepareMove(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	source, err := m.resolveSource(ctx, req, OperationMove)
	if err != nil {
		return ChangeSet{}, err
	}
	target, err := m.resolveTargetPath(ctx, req.TargetPath, OperationMove)
	if err != nil {
		return ChangeSet{}, err
	}
	if err := m.ensureTargetAbsent(target); err != nil {
		return ChangeSet{}, err
	}
	if err := m.ensureParentSafe(target); err != nil {
		return ChangeSet{}, err
	}
	hash, fileID, size, err := m.snapshotForExisting(source)
	if err != nil {
		return ChangeSet{}, err
	}
	cs := m.newChangeSet(req, ChangeOpMove)
	cs.Source = source
	cs.Target = target
	cs.TargetDisplayPath = target.DisplayPath
	cs.BaselineHash = hash
	cs.BaselineFileID = fileID
	cs.Preview = ChangePreview{
		Summary:       "move " + source.DisplayPath + " to " + target.DisplayPath,
		Bytes:         size,
		AffectedFiles: 1,
		Ops: []ChangeOp{{
			Index:       0,
			Operation:   ChangeOpMove,
			SourcePath:  source.DisplayPath,
			TargetPath:  target.DisplayPath,
			SourceHash:  hash,
			Bytes:       size,
			Description: "move resource",
		}},
	}
	cs.PlanHash = planHash(cs)
	return cs, nil
}

func (m *ChangeSetManager) prepareDelete(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	source, err := m.resolveSource(ctx, req, OperationDelete)
	if err != nil {
		return ChangeSet{}, err
	}
	hash, fileID, size, err := m.snapshotForExisting(source)
	if err != nil {
		return ChangeSet{}, err
	}
	if source.ResourceType == "directory" {
		if req.Recursive {
			ops, bytes, err := m.recursiveDeleteOps(source)
			if err != nil {
				return ChangeSet{}, err
			}
			cs := m.newChangeSet(req, ChangeOpDelete)
			cs.Source = source
			cs.TargetDisplayPath = source.DisplayPath
			cs.BaselineHash = hash
			cs.BaselineFileID = fileID
			cs.QuarantinePath = quarantinePathFor(m.quarantineDir, cs.ID, source.CanonicalPath, req.PermanentDelete)
			cs.Preview = ChangePreview{
				Summary:       deleteSummary(source.DisplayPath, req.PermanentDelete, true),
				Bytes:         bytes,
				AffectedFiles: len(ops),
				Ops:           ops,
			}
			cs.PlanHash = planHash(cs)
			return cs, nil
		}
		if err := ensureEmptyDir(source.CanonicalPath); err != nil {
			return ChangeSet{}, err
		}
	}
	cs := m.newChangeSet(req, ChangeOpDelete)
	cs.Source = source
	cs.TargetDisplayPath = source.DisplayPath
	cs.BaselineHash = hash
	cs.BaselineFileID = fileID
	cs.QuarantinePath = quarantinePathFor(m.quarantineDir, cs.ID, source.CanonicalPath, req.PermanentDelete)
	cs.Preview = ChangePreview{
		Summary:       deleteSummary(source.DisplayPath, req.PermanentDelete, false),
		Bytes:         size,
		AffectedFiles: 1,
		Ops: []ChangeOp{{
			Index:       0,
			Operation:   ChangeOpDelete,
			SourcePath:  source.DisplayPath,
			SourceHash:  hash,
			Bytes:       size,
			Description: deleteDescription(req.PermanentDelete, false),
		}},
	}
	cs.PlanHash = planHash(cs)
	return cs, nil
}

func (m *ChangeSetManager) prepareMkdir(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	target, err := m.resolveTarget(ctx, req, OperationMkdir)
	if err != nil {
		return ChangeSet{}, err
	}
	if err := m.ensureTargetAbsent(target); err != nil {
		return ChangeSet{}, err
	}
	if err := m.ensureParentSafe(target); err != nil {
		return ChangeSet{}, err
	}
	cs := m.newChangeSet(req, ChangeOpMkdir)
	cs.Target = target
	cs.TargetDisplayPath = target.DisplayPath
	cs.Preview = ChangePreview{
		Summary:       "mkdir " + target.DisplayPath,
		AffectedFiles: 1,
		Ops: []ChangeOp{{
			Index:       0,
			Operation:   ChangeOpMkdir,
			TargetPath:  target.DisplayPath,
			Description: "create directory",
		}},
	}
	cs.PlanHash = planHash(cs)
	return cs, nil
}

func (m *ChangeSetManager) prepareRmdir(ctx context.Context, req ChangeSetRequest) (ChangeSet, error) {
	source, err := m.resolveSource(ctx, req, OperationRmdir)
	if err != nil {
		return ChangeSet{}, err
	}
	info, err := os.Lstat(source.CanonicalPath)
	if err != nil {
		return ChangeSet{}, err
	}
	if !info.IsDir() {
		return ChangeSet{}, fmt.Errorf("rmdir target is not a directory")
	}
	if err := ensureEmptyDir(source.CanonicalPath); err != nil {
		return ChangeSet{}, err
	}
	hash, fileID, _, err := m.snapshotForExisting(source)
	if err != nil {
		return ChangeSet{}, err
	}
	cs := m.newChangeSet(req, ChangeOpRmdir)
	cs.Source = source
	cs.TargetDisplayPath = source.DisplayPath
	cs.BaselineHash = hash
	cs.BaselineFileID = fileID
	cs.QuarantinePath = filepath.Join(m.quarantineDir, cs.ID+"-"+filepath.Base(source.CanonicalPath))
	cs.Preview = ChangePreview{
		Summary:       "rmdir " + source.DisplayPath + " into quarantine",
		AffectedFiles: 1,
		Ops: []ChangeOp{{
			Index:       0,
			Operation:   ChangeOpRmdir,
			SourcePath:  source.DisplayPath,
			SourceHash:  hash,
			Description: "quarantine empty directory",
		}},
	}
	cs.PlanHash = planHash(cs)
	return cs, nil
}

func (m *ChangeSetManager) newChangeSet(req ChangeSetRequest, op ChangeOperation) ChangeSet {
	now := time.Now().UTC()
	return ChangeSet{
		ID:              "cs-" + uuid.NewString(),
		Principal:       req.Principal,
		Status:          ChangeSetStatusApprovalPending,
		Operation:       op,
		PermanentDelete: req.PermanentDelete,
		Recursive:       req.Recursive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

type fileSnapshot struct {
	hash   string
	fileID string
	data   []byte
	size   int64
}

func (m *ChangeSetManager) existingFileSnapshot(ref ResourceRef) (fileSnapshot, error) {
	info, err := os.Lstat(ref.CanonicalPath)
	if err != nil {
		return fileSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fileSnapshot{}, fmt.Errorf("target is a symlink")
	}
	if info.IsDir() {
		return fileSnapshot{}, fmt.Errorf("target is a directory")
	}
	if info.Size() > m.maxBytes {
		return fileSnapshot{}, fmt.Errorf("target too large (%d bytes)", info.Size())
	}
	data, err := os.ReadFile(ref.CanonicalPath)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{
		hash:   "sha256:" + sha256Hex(data),
		fileID: fileIdentity(ref.CanonicalPath, info),
		data:   data,
		size:   info.Size(),
	}, nil
}

func (m *ChangeSetManager) snapshotForExisting(ref ResourceRef) (string, string, int64, error) {
	info, err := os.Lstat(ref.CanonicalPath)
	if err != nil {
		return "", "", 0, err
	}
	if info.IsDir() {
		return "sha256:" + sha256Hex([]byte(fileIdentity(ref.CanonicalPath, info))), fileIdentity(ref.CanonicalPath, info), 0, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "sha256:" + sha256Hex([]byte(fileIdentity(ref.CanonicalPath, info))), fileIdentity(ref.CanonicalPath, info), 0, nil
	}
	if info.Size() > m.maxBytes {
		return "", "", 0, fmt.Errorf("target too large (%d bytes)", info.Size())
	}
	data, err := os.ReadFile(ref.CanonicalPath)
	if err != nil {
		return "", "", 0, err
	}
	return "sha256:" + sha256Hex(data), fileIdentity(ref.CanonicalPath, info), info.Size(), nil
}

func (m *ChangeSetManager) stageForRequest(ctx context.Context, req ChangeSetRequest) (StagedResource, error) {
	if strings.TrimSpace(req.StagingID) != "" {
		path, err := m.stagingPathForID(req.StagingID)
		if err != nil {
			return StagedResource{}, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return StagedResource{}, fmt.Errorf("read staged resource: %w", err)
		}
		return StagedResource{
			ID:          req.StagingID,
			Path:        path,
			ContentHash: "sha256:" + sha256Hex(data),
			Bytes:       int64(len(data)),
			CreatedAt:   time.Now().UTC(),
		}, nil
	}
	return m.StageResource(ctx, req.Content)
}

func (m *ChangeSetManager) resolveSource(ctx context.Context, req ChangeSetRequest, operation string) (ResourceRef, error) {
	selector := req.Resource
	if selector.Kind == "" {
		selector = selectorFromPathOrID(req.Path, req.ResourceID)
	}
	if err := m.rejectSymlinkSelector(selector); err != nil {
		return ResourceRef{}, err
	}
	return m.broker.resolveRefForOperation(ctx, selector, operation)
}

func (m *ChangeSetManager) resolveTarget(ctx context.Context, req ChangeSetRequest, operation string) (ResourceRef, error) {
	selector := req.TargetResource
	if selector.Kind == "" {
		selector = selectorFromPathOrID(req.Path, req.ResourceID)
	}
	return m.broker.resolveRefForOperation(ctx, selector, operation)
}

func (m *ChangeSetManager) resolveTargetPath(ctx context.Context, path string, operation string) (ResourceRef, error) {
	return m.broker.resolveRefForOperation(ctx, ResourceSelector{Kind: ResourceSelectorAlias, Path: path}, operation)
}

func selectorFromPathOrID(path, resourceID string) ResourceSelector {
	if strings.TrimSpace(resourceID) != "" {
		return ResourceSelector{Kind: ResourceSelectorResourceID, ID: strings.TrimSpace(resourceID)}
	}
	if strings.HasPrefix(strings.TrimSpace(path), "@") {
		return ResourceSelector{Kind: ResourceSelectorAlias, Path: path}
	}
	return ResourceSelector{Kind: ResourceSelectorPath, Path: path}
}

func (m *ChangeSetManager) ensureTargetAbsent(ref ResourceRef) error {
	if _, err := os.Lstat(ref.CanonicalPath); err == nil {
		return fmt.Errorf("target already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *ChangeSetManager) ensureParentSafe(ref ResourceRef) error {
	root, ok := m.broker.roots[ref.RootID]
	if !ok {
		return fmt.Errorf("host resource root %q is not configured", ref.RootID)
	}
	parent := filepath.Dir(ref.CanonicalPath)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve target parent: %w", err)
	}
	if !isPathWithin(root.RealPath, filepath.Clean(realParent)) {
		return fmt.Errorf("target parent escapes host resource root")
	}
	if protectedPathReason(ref.CanonicalPath) != "" {
		return fmt.Errorf("protected target path denied")
	}
	return nil
}

func (m *ChangeSetManager) applyCreateFile(cs ChangeSet) (ChangeSet, error) {
	if err := m.ensureTargetAbsent(cs.Target); err != nil {
		return cs, conflict(err.Error())
	}
	if err := m.ensureParentSafe(cs.Target); err != nil {
		return cs, conflict(err.Error())
	}
	data, err := m.verifiedStagedData(cs)
	if err != nil {
		return cs, conflict(err.Error())
	}
	if err := atomicWriteFile(cs.Target.CanonicalPath, data, 0o644); err != nil {
		return cs, err
	}
	return cs, nil
}

func (m *ChangeSetManager) applyOverwriteFile(cs ChangeSet) (ChangeSet, error) {
	if err := m.verifyBaseline(cs.Source, cs.BaselineHash, cs.BaselineFileID); err != nil {
		return cs, conflict(err.Error())
	}
	if err := m.ensureParentSafe(cs.Source); err != nil {
		return cs, conflict(err.Error())
	}
	data, err := m.verifiedStagedData(cs)
	if err != nil {
		return cs, conflict(err.Error())
	}
	mode := os.FileMode(0o644)
	if info, err := os.Lstat(cs.Source.CanonicalPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := atomicWriteFile(cs.Source.CanonicalPath, data, mode); err != nil {
		return cs, err
	}
	return cs, nil
}

func (m *ChangeSetManager) applyMove(cs ChangeSet) (ChangeSet, error) {
	if err := m.verifyBaseline(cs.Source, cs.BaselineHash, cs.BaselineFileID); err != nil {
		return cs, conflict(err.Error())
	}
	if err := m.ensureTargetAbsent(cs.Target); err != nil {
		return cs, conflict(err.Error())
	}
	if err := m.ensureParentSafe(cs.Target); err != nil {
		return cs, conflict(err.Error())
	}
	if err := os.Rename(cs.Source.CanonicalPath, cs.Target.CanonicalPath); err == nil {
		return cs, nil
	}
	if err := copyFileVerifyRemove(cs.Source.CanonicalPath, cs.Target.CanonicalPath, cs.BaselineHash); err != nil {
		return cs, err
	}
	return cs, nil
}

func (m *ChangeSetManager) applyDelete(cs ChangeSet) (ChangeSet, error) {
	if err := m.verifyBaseline(cs.Source, cs.BaselineHash, cs.BaselineFileID); err != nil {
		return cs, conflict(err.Error())
	}
	if cs.Recursive {
		if err := m.verifyRecursiveBaseline(cs); err != nil {
			return cs, conflict(err.Error())
		}
	}
	if cs.PermanentDelete {
		if cs.Recursive {
			if err := os.RemoveAll(cs.Source.CanonicalPath); err != nil {
				return cs, err
			}
			return cs, nil
		}
		if err := os.Remove(cs.Source.CanonicalPath); err != nil {
			return cs, err
		}
		return cs, nil
	}
	if err := os.MkdirAll(filepath.Dir(cs.QuarantinePath), 0o700); err != nil {
		return cs, err
	}
	if err := os.Rename(cs.Source.CanonicalPath, cs.QuarantinePath); err != nil {
		return cs, err
	}
	return cs, nil
}

func (m *ChangeSetManager) applyMkdir(cs ChangeSet) (ChangeSet, error) {
	if err := m.ensureTargetAbsent(cs.Target); err != nil {
		return cs, conflict(err.Error())
	}
	if err := m.ensureParentSafe(cs.Target); err != nil {
		return cs, conflict(err.Error())
	}
	if err := os.Mkdir(cs.Target.CanonicalPath, 0o755); err != nil {
		return cs, err
	}
	return cs, nil
}

func (m *ChangeSetManager) applyRmdir(cs ChangeSet) (ChangeSet, error) {
	if err := m.verifyBaseline(cs.Source, cs.BaselineHash, cs.BaselineFileID); err != nil {
		return cs, conflict(err.Error())
	}
	if err := ensureEmptyDir(cs.Source.CanonicalPath); err != nil {
		return cs, conflict(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(cs.QuarantinePath), 0o700); err != nil {
		return cs, err
	}
	if err := os.Rename(cs.Source.CanonicalPath, cs.QuarantinePath); err != nil {
		return cs, err
	}
	return cs, nil
}

func (m *ChangeSetManager) verifyBaseline(ref ResourceRef, wantHash, wantFileID string) error {
	gotHash, gotFileID, _, err := m.snapshotForExisting(ref)
	if err != nil {
		return err
	}
	if wantFileID != "" && gotFileID != wantFileID {
		return fmt.Errorf("baseline file identity changed")
	}
	if wantHash != "" && gotHash != wantHash {
		return fmt.Errorf("baseline hash changed")
	}
	return nil
}

func (m *ChangeSetManager) verifiedStagedData(cs ChangeSet) ([]byte, error) {
	data, err := os.ReadFile(cs.StagingPath)
	if err != nil {
		return nil, fmt.Errorf("read staged content: %w", err)
	}
	if int64(len(data)) > m.maxBytes {
		return nil, fmt.Errorf("staged content too large (%d bytes)", len(data))
	}
	hash := "sha256:" + sha256Hex(data)
	if hash != cs.ContentHash {
		return nil, fmt.Errorf("staged content hash mismatch")
	}
	return data, nil
}

func (m *ChangeSetManager) diffForChangeSet(cs ChangeSet) (string, bool) {
	newData, err := os.ReadFile(cs.StagingPath)
	if err != nil {
		return "", false
	}
	var oldData []byte
	if cs.Operation == ChangeOpOverwriteFile {
		oldData, _ = os.ReadFile(cs.Source.CanonicalPath)
	}
	return m.diffBytes(oldData, newData)
}

func (m *ChangeSetManager) diffBytes(oldData, newData []byte) (string, bool) {
	if (len(oldData) > 0 && !utf8.Valid(oldData)) || (len(newData) > 0 && !utf8.Valid(newData)) {
		return "", true
	}
	oldText := truncateDiffText(string(oldData))
	newText := truncateDiffText(string(newData))
	if oldText == newText {
		return "", false
	}
	return "--- before\n+++ after\n@@\n-" + oldText + "\n+" + newText, false
}

func truncateDiffText(value string) string {
	const limit = 4000
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n...<truncated>"
}

func (m *ChangeSetManager) save(ctx context.Context, cs ChangeSet) error {
	if m.store != nil {
		if err := m.store.Save(ctx, cs); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory[cs.ID] = cs
	return nil
}

func (m *ChangeSetManager) update(ctx context.Context, cs ChangeSet) error {
	if m.store != nil {
		if err := m.store.Update(ctx, cs); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory[cs.ID] = cs
	return nil
}

func (m *ChangeSetManager) get(ctx context.Context, id string) (ChangeSet, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ChangeSet{}, false, fmt.Errorf("changeset id is required")
	}
	if m.store != nil {
		cs, ok, err := m.store.Get(ctx, id)
		if err != nil || ok {
			return cs, ok, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cs, ok := m.memory[id]
	return cs, ok, nil
}

func (m *ChangeSetManager) ListChanges(ctx context.Context, statuses []ChangeSetStatus) ([]ChangeSet, error) {
	if m == nil {
		return nil, fmt.Errorf("host resource changeset manager is unavailable")
	}
	if m.store != nil {
		return m.store.List(ctx, statuses)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ChangeSet, 0, len(m.memory))
	for _, cs := range m.memory {
		if len(statuses) == 0 || containsChangeSetStatus(statuses, cs.Status) {
			out = append(out, cs)
		}
	}
	return out, nil
}

func (m *ChangeSetManager) markConflict(ctx context.Context, cs ChangeSet, message string) (ChangeSet, error) {
	cs.Status = ChangeSetStatusConflict
	cs.ErrorMessage = message
	cs.UpdatedAt = time.Now().UTC()
	if err := m.update(ctx, cs); err != nil {
		return ChangeSet{}, err
	}
	return cs, nil
}

func validateApplyOptions(cs ChangeSet, opts ChangeApplyOptions) error {
	deleteMode := strings.TrimSpace(opts.DeleteMode)
	if deleteMode == "" {
		deleteMode = DeleteModeQuarantine
	}
	switch deleteMode {
	case DeleteModeQuarantine, DeleteModePermanent:
	default:
		return fmt.Errorf("unsupported delete_mode %q", opts.DeleteMode)
	}
	if cs.Operation == ChangeOpDelete || cs.Operation == ChangeOpRmdir {
		if cs.PermanentDelete {
			if deleteMode != DeleteModePermanent {
				return fmt.Errorf("permanent delete requires explicit delete_mode=permanent approval")
			}
		} else if deleteMode != DeleteModeQuarantine {
			return fmt.Errorf("delete_mode=%s does not match quarantine changeset", deleteMode)
		}
	} else if opts.DeleteMode != "" {
		return fmt.Errorf("delete_mode is only valid for delete/rmdir changesets")
	}
	if cs.Recursive && !opts.Recursive {
		return fmt.Errorf("recursive changeset requires explicit recursive approval")
	}
	if !cs.Recursive && opts.Recursive {
		return fmt.Errorf("recursive approval does not match non-recursive changeset")
	}
	return nil
}

func containsChangeSetStatus(statuses []ChangeSetStatus, want ChangeSetStatus) bool {
	for _, status := range statuses {
		if status == want {
			return true
		}
	}
	return false
}

func (m *ChangeSetManager) stagingBlobDir() string {
	return filepath.Join(m.stagingDir, "blobs")
}

func (m *ChangeSetManager) stagingPathForID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || !strings.HasPrefix(id, "stage-") {
		return "", fmt.Errorf("invalid staging_id")
	}
	return filepath.Join(m.stagingBlobDir(), id+".blob"), nil
}

func (m *ChangeSetManager) rejectSymlinkSelector(selector ResourceSelector) error {
	if selector.Kind == ResourceSelectorResourceID {
		if ref, ok := m.broker.refs[selector.ID]; ok && ref.ResourceType == "symlink" {
			return fmt.Errorf("symlink resource targets cannot be changed")
		}
		return nil
	}
	path := strings.TrimSpace(selector.Path)
	if path == "" {
		path = strings.TrimSpace(selector.DisplayPath)
	}
	if path == "" || selector.Kind == ResourceSelectorResourceID {
		return nil
	}
	candidate, root, ok := m.rawCandidateForSelector(path, selector.RootID)
	if !ok {
		return nil
	}
	return rejectSymlinkTraversal(root.RealPath, candidate)
}

func (m *ChangeSetManager) rawCandidateForSelector(path, fallbackRootID string) (string, rootEntry, bool) {
	if strings.HasPrefix(path, "@") || fallbackRootID != "" {
		aliasPath := strings.TrimPrefix(path, "@")
		rootID := fallbackRootID
		rest := ""
		if aliasPath != "" {
			parts := strings.SplitN(filepath.ToSlash(aliasPath), "/", 2)
			rootID = parts[0]
			if len(parts) == 2 {
				rest = parts[1]
			}
		}
		root, ok := m.broker.roots[rootID]
		if !ok {
			return "", rootEntry{}, false
		}
		candidate := root.RealPath
		if rest != "" {
			candidate = filepath.Join(root.RealPath, filepath.FromSlash(rest))
		}
		return filepath.Clean(candidate), root, true
	}
	if filepath.IsAbs(path) {
		candidate := filepath.Clean(path)
		root, ok := m.broker.rootForPath(evalPathIfExists(candidate))
		return candidate, root, ok
	}
	return "", rootEntry{}, false
}

func rejectSymlinkTraversal(root, candidate string) error {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	if !isPathWithin(root, candidate) {
		return nil
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink resource targets cannot be changed")
		}
	}
	return nil
}

func (m *ChangeSetManager) recursiveDeleteOps(source ResourceRef) ([]ChangeOp, int64, error) {
	root, ok := m.broker.roots[source.RootID]
	if !ok {
		return nil, 0, fmt.Errorf("host resource root %q is not configured", source.RootID)
	}
	var ops []ChangeOp
	var totalBytes int64
	err := filepath.WalkDir(source.CanonicalPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		hash, size, err := hashForPlanEntry(path, info, m.maxBytes)
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			totalBytes += size
			if totalBytes > m.maxBytes {
				return fmt.Errorf("recursive delete tree too large (%d bytes)", totalBytes)
			}
		}
		ops = append(ops, ChangeOp{
			Index:       len(ops),
			Operation:   ChangeOpDelete,
			SourcePath:  displayPathFor(root, path),
			SourceHash:  hash,
			Bytes:       size,
			Description: "recursive quarantine delete",
		})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return ops, totalBytes, nil
}

func (m *ChangeSetManager) verifyRecursiveBaseline(cs ChangeSet) error {
	ops, bytes, err := m.recursiveDeleteOps(cs.Source)
	if err != nil {
		return err
	}
	if bytes != cs.Preview.Bytes || len(ops) != len(cs.Preview.Ops) {
		return fmt.Errorf("recursive baseline changed")
	}
	for i := range ops {
		want := cs.Preview.Ops[i]
		got := ops[i]
		if got.SourcePath != want.SourcePath || got.SourceHash != want.SourceHash || got.Bytes != want.Bytes {
			return fmt.Errorf("recursive baseline changed")
		}
	}
	return nil
}

func hashForPlanEntry(path string, info os.FileInfo, maxBytes int64) (string, int64, error) {
	if info == nil {
		return "", 0, fmt.Errorf("missing file info")
	}
	if info.IsDir() {
		return "sha256:" + sha256Hex([]byte("dir:"+filepath.ToSlash(path))), 0, nil
	}
	if info.Mode().IsRegular() {
		if info.Size() > maxBytes {
			return "", 0, fmt.Errorf("target too large (%d bytes)", info.Size())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", 0, err
		}
		return "sha256:" + sha256Hex(data), info.Size(), nil
	}
	return "sha256:" + sha256Hex([]byte(fileIdentity(path, info))), 0, nil
}

func quarantinePathFor(quarantineDir, id, source string, permanent bool) string {
	if permanent {
		return ""
	}
	return filepath.Join(quarantineDir, id+"-"+filepath.Base(source))
}

func deleteSummary(displayPath string, permanent, recursive bool) string {
	mode := "into quarantine"
	if permanent {
		mode = "permanently"
	}
	if recursive {
		return "delete " + displayPath + " recursively " + mode
	}
	return "delete " + displayPath + " " + mode
}

func deleteDescription(permanent, recursive bool) string {
	switch {
	case permanent && recursive:
		return "recursive permanent delete"
	case permanent:
		return "permanent delete"
	case recursive:
		return "recursive quarantine delete"
	default:
		return "quarantine delete"
	}
}

func planHash(cs ChangeSet) string {
	plan := struct {
		Operation          ChangeOperation `json:"operation"`
		SourceID           string          `json:"source_id,omitempty"`
		SourcePathHash     string          `json:"source_path_hash,omitempty"`
		TargetPathHash     string          `json:"target_path_hash,omitempty"`
		BaselineHash       string          `json:"baseline_hash,omitempty"`
		BaselineFileID     string          `json:"baseline_file_id,omitempty"`
		ContentHash        string          `json:"content_hash,omitempty"`
		PermanentDelete    bool            `json:"permanent_delete,omitempty"`
		Recursive          bool            `json:"recursive,omitempty"`
		QuarantinePathHash string          `json:"quarantine_path_hash,omitempty"`
		Ops                []ChangeOp      `json:"ops"`
	}{
		Operation:          cs.Operation,
		SourceID:           cs.Source.ID,
		SourcePathHash:     cs.Source.CanonicalPathHash,
		TargetPathHash:     cs.Target.CanonicalPathHash,
		BaselineHash:       cs.BaselineHash,
		BaselineFileID:     cs.BaselineFileID,
		ContentHash:        cs.ContentHash,
		PermanentDelete:    cs.PermanentDelete,
		Recursive:          cs.Recursive,
		QuarantinePathHash: "sha256:" + sha256Hex([]byte(cs.QuarantinePath)),
		Ops:                cs.Preview.Ops,
	}
	raw, _ := json.Marshal(plan)
	return "sha256:" + sha256Hex(raw)
}

type conflictError string

func conflict(message string) error {
	return conflictError("conflict: " + message)
}

func (e conflictError) Error() string {
	return string(e)
}

func isConflictError(err error) bool {
	_, ok := err.(conflictError)
	return ok
}

func ensureEmptyDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("directory is not empty")
	}
	return nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	tmp, err := os.CreateTemp(parent, ".emo-changeset-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Sync()
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func copyFileVerifyRemove(source, target, expectedHash string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cross-volume symlink move is unavailable")
	}
	if info.IsDir() {
		return fmt.Errorf("cross-volume directory move is unavailable")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		_ = in.Close()
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = in.Close()
		_ = out.Close()
		return err
	}
	if err := in.Close(); err != nil {
		_ = out.Close()
		return err
	}
	_ = out.Sync()
	if err := out.Close(); err != nil {
		return err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if "sha256:"+sha256Hex(data) != expectedHash {
		_ = os.Remove(target)
		return fmt.Errorf("moved file hash verification failed")
	}
	return os.Remove(source)
}

func mustReadFile(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
