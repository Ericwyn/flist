package service

import (
	"context"
	"errors"
	"path"
	"strconv"
	"strings"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// ConflictPolicy controls what a copy / move does when its destination already exists.
type ConflictPolicy string

const (
	ConflictError     ConflictPolicy = model.ConflictError
	ConflictRename    ConflictPolicy = model.ConflictRename
	ConflictMergeDirs ConflictPolicy = model.ConflictMergeDirs
)

// TransferOperation identifies the primitive operation used by the transfer engine.
type TransferOperation string

const (
	TransferCopy TransferOperation = model.FileOpCopy
	TransferMove TransferOperation = model.FileOpMove
)

// ParseConflictPolicy translates the new policy field and the legacy auto_rename flag.
// An omitted policy intentionally preserves the old API semantics.
func ParseConflictPolicy(raw string, autoRename bool) (ConflictPolicy, error) {
	if strings.TrimSpace(raw) == "" {
		if autoRename {
			return ConflictRename, nil
		}
		return ConflictError, nil
	}
	switch ConflictPolicy(raw) {
	case ConflictError, ConflictRename, ConflictMergeDirs:
		return ConflictPolicy(raw), nil
	default:
		return "", storage.ErrBadOp
	}
}

// ConflictItem is a top-level conflict returned by the lightweight preflight.
type ConflictItem struct {
	Src        string `json:"src"`
	Target     string `json:"target"`
	SourceType string `json:"source_type"`
	TargetType string `json:"target_type"`
	Kind       string `json:"kind"`
}

// TransferInspection is deliberately shallow: recursive conflicts are resolved while executing.
type TransferInspection struct {
	HasConflicts       bool           `json:"has_conflicts"`
	DirectoryConflicts int            `json:"directory_conflicts"`
	OtherConflicts     int            `json:"other_conflicts"`
	Items              []ConflictItem `json:"items,omitempty"`
}

// InspectTransfer checks source and destination paths and reports top-level collisions.
// It also catches duplicate target names within one batch before an async task is queued.
func (s *FileService) InspectTransfer(ctx context.Context, operation TransferOperation, srcs []string, dst string) (*TransferInspection, error) {
	if operation != TransferCopy && operation != TransferMove {
		return nil, storage.ErrBadOp
	}
	caps := s.backend.Capabilities()
	if !caps.Write || (operation == TransferCopy && !caps.Copy) {
		return nil, storage.ErrNotSupported
	}
	if len(srcs) == 0 || strings.TrimSpace(dst) == "" {
		return nil, storage.ErrBadOp
	}
	cleanedDst := util.CleanAPIPath(dst)
	dstInfo, dstExists, err := s.statMaybe(ctx, cleanedDst)
	if err != nil {
		return nil, err
	}
	dstIsDir := dstExists && dstInfo.Type == model.TypeDir
	if !dstIsDir && len(srcs) != 1 {
		return nil, storage.ErrNotDir
	}

	inspection := &TransferInspection{Items: make([]ConflictItem, 0)}
	reserved := make(map[string]model.FileInfo, len(srcs))
	for _, src := range srcs {
		srcClean := util.CleanAPIPath(src)
		srcInfo, err := s.backend.Stat(ctx, srcClean)
		if err != nil {
			return nil, err
		}
		target := cleanedDst
		if dstIsDir {
			target = path.Join(cleanedDst, srcInfo.Name)
		}
		if srcInfo.Type == model.TypeDir && isSubtreePath(srcClean, target) {
			return nil, storage.ErrBadOp
		}

		targetInfo, targetExists, err := s.statMaybe(ctx, target)
		if err != nil {
			return nil, err
		}
		if prior, duplicate := reserved[target]; duplicate && !targetExists {
			// A source in the same batch occupies the target even though it is not on disk yet.
			targetInfo = &prior
			targetExists = true
		}
		reserved[target] = *srcInfo
		if !targetExists {
			parentInfo, parentExists, parentErr := s.statMaybe(ctx, path.Dir(target))
			if parentErr != nil {
				return nil, parentErr
			}
			if !parentExists {
				return nil, storage.ErrNotFound
			}
			if parentInfo.Type != model.TypeDir {
				return nil, storage.ErrNotDir
			}
			continue
		}

		item := ConflictItem{
			Src:        srcClean,
			Target:     target,
			SourceType: srcInfo.Type,
			TargetType: targetInfo.Type,
			Kind:       "rename",
		}
		if srcInfo.Type == model.TypeDir && targetInfo.Type == model.TypeDir {
			item.Kind = "directory_merge"
			inspection.DirectoryConflicts++
		} else {
			inspection.OtherConflicts++
		}
		inspection.Items = append(inspection.Items, item)
	}
	inspection.HasConflicts = len(inspection.Items) > 0
	return inspection, nil
}

// Transfer executes a batch using the shared recursive transfer engine.
func (s *FileService) Transfer(ctx context.Context, operation TransferOperation, srcs []string, dst string, policy ConflictPolicy, progress func(index int, copied int64)) []model.OpResult {
	results := make([]model.OpResult, 0, len(srcs))
	cleanedDst := util.CleanAPIPath(dst)
	dstInfo, dstExists, dstErr := s.statMaybe(ctx, cleanedDst)
	if dstErr != nil {
		for _, src := range srcs {
			results = append(results, opFail(util.CleanAPIPath(src), dstErr))
		}
		return results
	}
	dstIsDir := dstExists && dstInfo.Type == model.TypeDir
	single := len(srcs) == 1

	for i, src := range srcs {
		srcClean := util.CleanAPIPath(src)
		if !dstIsDir && !single {
			results = append(results, opFail(srcClean, storage.ErrNotDir))
			continue
		}
		target := cleanedDst
		if dstIsDir {
			target = path.Join(cleanedDst, path.Base(srcClean))
		}
		itemPolicy := policy
		// A concrete destination path is the rename/copy-to-name form; retain
		// the historical strict-conflict behavior there. Auto-renaming applies
		// only when entering an existing directory.
		if !dstIsDir {
			itemPolicy = ConflictError
		}
		var cb func(int64)
		if progress != nil {
			cb = func(copied int64) { progress(i, copied) }
		}
		results = append(results, s.TransferOne(ctx, operation, srcClean, target, itemPolicy, cb))
	}
	return results
}

// TransferOne moves or copies one source to an already-resolved target path.
// The target is never overwritten; merge_dirs is the only policy that descends into it.
func (s *FileService) TransferOne(ctx context.Context, operation TransferOperation, src, target string, policy ConflictPolicy, progress func(int64)) model.OpResult {
	srcClean := util.CleanAPIPath(src)
	targetClean := util.CleanAPIPath(target)
	if operation != TransferCopy && operation != TransferMove {
		return opFail(srcClean, storage.ErrBadOp)
	}
	if policy != ConflictError && policy != ConflictRename && policy != ConflictMergeDirs {
		return opFail(srcClean, storage.ErrBadOp)
	}
	actualTarget := targetClean
	outcome, changed, err := s.transferNode(ctx, operation, srcClean, targetClean, policy, progress, &actualTarget)
	if err != nil {
		result := opFail(srcClean, err)
		result.Target = actualTarget
		if changed {
			result.Outcome = "partial"
		} else if outcome != "" {
			result.Outcome = outcome
		}
		return result
	}
	return model.OpResult{Src: srcClean, OK: true, Target: actualTarget, Outcome: outcome}
}

func (s *FileService) transferNode(ctx context.Context, operation TransferOperation, src, target string, policy ConflictPolicy, progress func(int64), actualTarget *string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	srcInfo, err := s.backend.Stat(ctx, src)
	if err != nil {
		return "", false, err
	}
	if srcInfo.Type == model.TypeDir && isSubtreePath(src, target) {
		return "", false, storage.ErrBadOp
	}
	targetInfo, targetExists, err := s.statMaybe(ctx, target)
	if err != nil {
		return "", false, err
	}
	if !targetExists {
		if err := s.prepareCopy(ctx, operation, src, path.Dir(target)); err != nil {
			return "", false, err
		}
		if err := s.primitiveTransfer(ctx, operation, src, target, progress); err != nil {
			return "", false, err
		}
		if operation == TransferCopy {
			return "copied", true, nil
		}
		return "moved", true, nil
	}

	if policy == ConflictError {
		return "", false, storage.ErrExists
	}
	if policy == ConflictMergeDirs && srcInfo.Type == model.TypeDir && targetInfo.Type == model.TypeDir {
		items, err := s.backend.List(ctx, src, true)
		if err != nil {
			return "", false, err
		}
		changed := false
		for _, item := range items {
			childTarget := path.Join(target, item.Name)
			_, childChanged, childErr := s.transferNode(ctx, operation, path.Join(src, item.Name), childTarget, policy, progress, &childTarget)
			changed = changed || childChanged
			if childErr != nil {
				if changed {
					return "partial", true, childErr
				}
				return "", false, childErr
			}
		}
		if operation == TransferMove {
			if err := s.backend.Remove(ctx, src); err != nil {
				if changed {
					return "partial", true, err
				}
				return "", false, err
			}
		}
		return "merged", true, nil
	}

	for attempt := 0; attempt < maxRenameProbe; attempt++ {
		conflictTarget, err := s.nextConflictTarget(ctx, path.Dir(target), path.Base(target), srcInfo.Type == model.TypeDir)
		if err != nil {
			return "", false, err
		}
		if err := s.prepareCopy(ctx, operation, src, path.Dir(conflictTarget)); err != nil {
			return "", false, err
		}
		if err := s.primitiveTransfer(ctx, operation, src, conflictTarget, progress); err != nil {
			// Another writer may have claimed the candidate after Stat. Re-probe
			// instead of reporting a false conflict; the backend still never overwrites.
			if errors.Is(err, storage.ErrExists) {
				continue
			}
			return "", false, err
		}
		*actualTarget = conflictTarget
		return "renamed", true, nil
	}
	return "", false, storage.ErrExists
}

func (s *FileService) primitiveTransfer(ctx context.Context, operation TransferOperation, src, target string, progress func(int64)) error {
	if progress != nil {
		if pc, ok := s.backend.(storage.ProgressCopier); ok {
			if operation == TransferCopy {
				return pc.CopyWithProgress(ctx, src, target, progress)
			}
			return pc.MoveWithProgress(ctx, src, target, progress)
		}
	}
	if operation == TransferCopy {
		return s.backend.Copy(ctx, src, target)
	}
	return s.backend.Move(ctx, src, target)
}

func (s *FileService) prepareCopy(ctx context.Context, operation TransferOperation, src, targetDir string) error {
	if operation == TransferCopy {
		return s.CheckSpace(ctx, src, targetDir)
	}
	return nil
}

func (s *FileService) statMaybe(ctx context.Context, p string) (*model.FileInfo, bool, error) {
	info, err := s.backend.Stat(ctx, p)
	if err == nil {
		return info, true, nil
	}
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (s *FileService) nextConflictTarget(ctx context.Context, dir, base string, isDir bool) (string, error) {
	original := path.Join(dir, base)
	if _, exists, err := s.statMaybe(ctx, original); err != nil {
		return "", err
	} else if !exists {
		return original, nil
	}
	for i := 2; i < maxRenameProbe+2; i++ {
		candidate := path.Join(dir, numberedName(base, isDir, i))
		_, exists, err := s.statMaybe(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", storage.ErrExists
}

// numberedName keeps directory names intact. Files retain the last extension.
func numberedName(name string, isDir bool, index int) string {
	suffix := " (" + strconv.Itoa(index) + ")"
	if isDir {
		return name + suffix
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" { // dotfile such as .env
		stem = name
		ext = ""
	}
	return stem + suffix + ext
}

func isSubtreePath(parent, child string) bool {
	parent = util.CleanAPIPath(parent)
	child = util.CleanAPIPath(child)
	return child == parent || strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}
