// Package service 的上传编排部分。
//
// UploadService 负责分片上传的「策略层」：在内存维护上传会话（已收分片、文件指纹、
// user 归属、活跃时间），做磁盘空间预检、文件名 / 大小校验、断点续传的指纹复用，以及
// 合并落盘时的路径级锁串行化。真正的分片字节落盘与拼接委托给实现了 storage.Uploader
// 的驱动（当前为 local）。会话状态存内存、随服务进程生命周期；服务重启后未完成上传
// 需重传，孤儿分片由后台 sweep（24h）兜底回收。
package service

import (
	"context"
	"errors"
	"io"
	"path"
	"sort"
	"strconv"
	"sync"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// 上传相关错误，handler 据此映射对外错误码（2009 / 2010 / 2011）。
var (
	ErrUploadTooLarge   = errors.New("upload exceeds max size")  // 2009
	ErrUploadNotFound   = errors.New("upload session not found") // 2010
	ErrUploadIncomplete = errors.New("upload is incomplete")     // 2011
)

const (
	minChunkSize     = 256 << 10 // 256 KiB
	maxChunkSize     = 64 << 20  // 64 MiB
	defaultChunkSize = 8 << 20   // 8 MiB
	maxTotalChunks   = 100000    // 防御异常的分片数（配合 chunk 大小约束总量）
)

// uploadSession 是一次上传的内存状态。received 的并发修改由 mu 保护。
type uploadSession struct {
	id          string
	userScope   string
	dir         string // 已 Clean 的目标目录 API 路径
	name        string
	fingerprint string
	totalSize   int64
	chunkSize   int64
	totalChunks int

	mu       sync.Mutex
	received map[int]bool
	lastSeen time.Time
}

// dstPath 返回该上传的目标文件 API 路径。
func (s *uploadSession) dstPath() string {
	return path.Join(s.dir, s.name)
}

// UploadService 提供分片上传编排，委托物理存取给实现 storage.Uploader 的 backend。
type UploadService struct {
	backend   storage.Backend
	locker    *util.PathLocker
	maxUpload int64 // 单文件上限（字节），0 表示不限

	mu        sync.Mutex
	sessions  map[string]*uploadSession // upload_id -> 会话
	byFinger  map[string]string         // 指纹键 -> upload_id（断点续传复用）
}

// NewUploadService 构造上传服务。backend 须实现 storage.Uploader，否则上传接口返回
// not_supported。locker 用于合并落盘的路径级串行化。maxUpload 为单文件字节上限（0 不限）。
func NewUploadService(backend storage.Backend, locker *util.PathLocker, maxUpload int64) *UploadService {
	return &UploadService{
		backend:   backend,
		locker:    locker,
		maxUpload: maxUpload,
		sessions:  make(map[string]*uploadSession),
		byFinger:  make(map[string]string),
	}
}

// uploader 返回 backend 的 Uploader 视图；不支持时返回 ErrNotSupported。
func (s *UploadService) uploader() (storage.Uploader, error) {
	u, ok := s.backend.(storage.Uploader)
	if !ok {
		return nil, storage.ErrNotSupported
	}
	return u, nil
}

// fingerKey 由会话标识维度拼成指纹键，用于断点续传时复用未完成会话。
func fingerKey(userScope, dir, name, fingerprint string, totalSize, chunkSize int64) string {
	return userScope + "\x00" + dir + "\x00" + name + "\x00" + fingerprint + "\x00" +
		strconv.FormatInt(totalSize, 10) + "\x00" + strconv.FormatInt(chunkSize, 10)
}

// normalizeChunkSize 将客户端请求的 chunkSize 钳制到 [minChunkSize, maxChunkSize]。
// <=0 时回落默认值。
func normalizeChunkSize(req int64) int64 {
	if req <= 0 {
		return defaultChunkSize
	}
	if req < minChunkSize {
		return minChunkSize
	}
	if req > maxChunkSize {
		return maxChunkSize
	}
	return req
}

// Init 初始化（或按指纹复用）一次上传会话。返回 upload_id、最终分片大小、总分片数与
// 已收分片索引（续传时非空）。
func (s *UploadService) Init(ctx context.Context, userScope, dir, name, fingerprint string, totalSize, chunkSize int64) (*model.UploadInitResult, error) {
	u, err := s.uploader()
	if err != nil {
		return nil, err
	}
	if totalSize <= 0 {
		return nil, storage.ErrBadOp
	}
	if s.maxUpload > 0 && totalSize > s.maxUpload {
		return nil, ErrUploadTooLarge
	}
	if err := util.ValidateName(name); err != nil {
		return nil, storage.ErrInvalidName
	}

	cleanDir := util.CleanAPIPath(dir)
	// 目标目录须为已存在目录。
	info, err := s.backend.Stat(ctx, cleanDir)
	if err != nil {
		return nil, err
	}
	if info.Type != model.TypeDir {
		return nil, storage.ErrNotDir
	}

	cs := normalizeChunkSize(chunkSize)
	totalChunks := int((totalSize + cs - 1) / cs)
	if totalChunks < 1 {
		totalChunks = 1
	}
	if totalChunks > maxTotalChunks {
		return nil, ErrUploadTooLarge
	}

	// 磁盘空间预检（仅当驱动支持 Usager）。
	if usager, ok := s.backend.(storage.Usager); ok {
		if _, free, uerr := usager.Usage(ctx, cleanDir); uerr == nil {
			if uint64(totalSize) > free {
				return nil, storage.ErrDiskFull
			}
		}
	}

	key := fingerKey(userScope, cleanDir, name, fingerprint, totalSize, cs)

	s.mu.Lock()
	// 指纹命中未完成会话 → 复用（断点续传）。
	if fingerprint != "" {
		if id, ok := s.byFinger[key]; ok {
			if sess, ok2 := s.sessions[id]; ok2 {
				s.mu.Unlock()
				return s.resumeResult(sess), nil
			}
			delete(s.byFinger, key) // 悬挂索引，清理
		}
	}

	id, terr := util.GenerateToken()
	if terr != nil {
		s.mu.Unlock()
		return nil, terr
	}
	sess := &uploadSession{
		id:          id,
		userScope:   userScope,
		dir:         cleanDir,
		name:        name,
		fingerprint: fingerprint,
		totalSize:   totalSize,
		chunkSize:   cs,
		totalChunks: totalChunks,
		received:    make(map[int]bool),
		lastSeen:    time.Now(),
	}
	s.sessions[id] = sess
	if fingerprint != "" {
		s.byFinger[key] = id
	}
	s.mu.Unlock()

	// 惰性创建暂存目录由 StageChunk 负责，这里无需触碰 uploader（仅断言其存在）。
	_ = u
	return &model.UploadInitResult{
		UploadID:    id,
		ChunkSize:   cs,
		TotalChunks: totalChunks,
		Received:    []int{},
	}, nil
}

// resumeResult 由已存在会话构造续传响应（含已收分片索引）。
func (s *UploadService) resumeResult(sess *uploadSession) *model.UploadInitResult {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return &model.UploadInitResult{
		UploadID:    sess.id,
		ChunkSize:   sess.chunkSize,
		TotalChunks: sess.totalChunks,
		Received:    sortedIndices(sess.received),
	}
}

// expectedChunkLen 返回第 index 个分片的应有字节数（末片可小于 chunkSize）。
func (sess *uploadSession) expectedChunkLen(index int) int64 {
	if index < sess.totalChunks-1 {
		return sess.chunkSize
	}
	last := sess.totalSize - int64(sess.totalChunks-1)*sess.chunkSize
	if last < 0 {
		return 0
	}
	return last
}

// Chunk 接收第 index 个分片，落盘并标记已收。reader 会被限制到该分片应有大小，
// 实际写入字节数与应有不符则拒绝（4000），不标记已收（便于重传纠正）。
func (s *UploadService) Chunk(ctx context.Context, uploadID string, index int, r io.Reader) (*model.UploadChunkResult, error) {
	u, err := s.uploader()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess, ok := s.sessions[uploadID]
	s.mu.Unlock()
	if !ok {
		return nil, ErrUploadNotFound
	}
	if index < 0 || index >= sess.totalChunks {
		return nil, storage.ErrBadOp
	}

	expected := sess.expectedChunkLen(index)
	// 限制读取到 expected+1，防止恶意超量分片填满暂存盘。
	limited := io.LimitReader(r, expected+1)
	n, werr := u.StageChunk(ctx, uploadID, index, limited)
	if werr != nil {
		return nil, werr
	}
	if n != expected {
		// 大小不符：清掉本次写坏的分片，不标记已收。
		_ = u.AbortChunk(uploadID, index)
		return nil, storage.ErrBadOp
	}

	sess.mu.Lock()
	sess.received[index] = true
	sess.lastSeen = time.Now()
	cnt := len(sess.received)
	sess.mu.Unlock()

	return &model.UploadChunkResult{Index: index, Received: cnt}, nil
}

// Complete 校验分片齐全后合并落盘。缺片返回 ErrUploadIncomplete（附 missing 索引）。
// 合并落点经路径级锁串行化，overwrite=false 且目标已存在则 ErrExists。
func (s *UploadService) Complete(ctx context.Context, uploadID string, overwrite bool) (*model.UploadCompleteResult, error) {
	u, err := s.uploader()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess, ok := s.sessions[uploadID]
	s.mu.Unlock()
	if !ok {
		return nil, ErrUploadNotFound
	}

	sess.mu.Lock()
	missing := missingIndices(sess.received, sess.totalChunks)
	sess.mu.Unlock()
	if len(missing) > 0 {
		return &model.UploadCompleteResult{Missing: missing}, ErrUploadIncomplete
	}

	dst := sess.dstPath()
	s.locker.Lock(dst)
	defer s.locker.Unlock(dst)

	if !overwrite {
		if _, serr := s.backend.Stat(ctx, dst); serr == nil {
			return nil, storage.ErrExists
		}
	}

	if err := u.MergeUpload(ctx, uploadID, dst, sess.totalChunks, overwrite); err != nil {
		return nil, err
	}

	s.dropSession(sess)
	return &model.UploadCompleteResult{Path: dst}, nil
}

// Abort 主动取消一次上传：删暂存区并移除会话（前端取消时调用，幂等）。
func (s *UploadService) Abort(uploadID string) {
	s.mu.Lock()
	sess, ok := s.sessions[uploadID]
	s.mu.Unlock()
	if !ok {
		return
	}
	if u, err := s.uploader(); err == nil {
		_ = u.AbortUpload(uploadID)
	}
	s.dropSession(sess)
}

// dropSession 从会话表与指纹索引移除一个会话。
func (s *UploadService) dropSession(sess *uploadSession) {
	s.mu.Lock()
	delete(s.sessions, sess.id)
	if sess.fingerprint != "" {
		key := fingerKey(sess.userScope, sess.dir, sess.name, sess.fingerprint, sess.totalSize, sess.chunkSize)
		if id, ok := s.byFinger[key]; ok && id == sess.id {
			delete(s.byFinger, key)
		}
	}
	s.mu.Unlock()
}

// Sweep 清理过期会话（lastSeen 早于 now-maxAge）及其暂存区，并触发驱动层的孤儿暂存清理。
// 返回清理的会话数。
func (s *UploadService) Sweep(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	var stale []*uploadSession

	s.mu.Lock()
	for _, sess := range s.sessions {
		sess.mu.Lock()
		old := sess.lastSeen.Before(cutoff)
		sess.mu.Unlock()
		if old {
			stale = append(stale, sess)
		}
	}
	s.mu.Unlock()

	u, _ := s.uploader()
	for _, sess := range stale {
		if u != nil {
			_ = u.AbortUpload(sess.id)
		}
		s.dropSession(sess)
	}
	// 兜底清理驱动层 mtime 超期的孤儿暂存目录（覆盖重启后丢失会话的情形）。
	if u != nil {
		_, _ = u.SweepStaging(maxAge)
	}
	return len(stale)
}

// sortedIndices 返回 received 集合的升序索引切片。
func sortedIndices(received map[int]bool) []int {
	out := make([]int, 0, len(received))
	for i, ok := range received {
		if ok {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

// missingIndices 返回 [0, total) 中不在 received 的索引（升序）。
func missingIndices(received map[int]bool, total int) []int {
	var out []int
	for i := 0; i < total; i++ {
		if !received[i] {
			out = append(out, i)
		}
	}
	return out
}
