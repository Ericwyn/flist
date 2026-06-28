package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// utf8BOM 是 UTF-8 字节序标记。读取时识别并保留，保存由上层原样写回。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ReadText 完整读取可编辑文本（storage.ContentEditor）。
//
// 校验顺序：解析路径 → 普通文件 → 大小上限 → 读取全部 → 文本嗅探 → 计算 revision。
// maxBytes <= 0 表示不限大小。非文本返回 ErrUnsupportedMedia，超限返回 ErrFileTooLarge。
func (b *Local) ReadText(_ context.Context, p string, maxBytes int64) (*model.FileContentResult, error) {
	local, cleaned, err := b.resolve(p)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, mapErr(err)
	}
	if fi.IsDir() {
		return nil, storage.ErrNotFile
	}
	if !fi.Mode().IsRegular() {
		return nil, storage.ErrNotFile
	}
	if maxBytes > 0 && fi.Size() > maxBytes {
		return nil, storage.ErrFileTooLarge
	}

	data, err := os.ReadFile(local)
	if err != nil {
		return nil, mapErr(err)
	}
	if !isEditableText(fi.Name(), data) {
		return nil, storage.ErrUnsupportedMedia
	}

	encoding, content := decodeEncoding(data)
	readonly := !isWritable(fi.Mode())

	return &model.FileContentResult{
		Path:       cleaned,
		Name:       fi.Name(),
		Size:       fi.Size(),
		MIME:       detectMIME(fi.Name()),
		Encoding:   encoding,
		LineEnding: detectLineEnding(content),
		Content:    content,
		ModTime:    fi.ModTime(),
		Revision:   revisionOf(data),
		Editable:   !readonly,
		Readonly:   readonly,
	}, nil
}

// WriteText 以乐观锁保存文本（storage.ContentEditor）。
//
// 流程：解析路径并确认普通文件 → 校验可写 → 重新读取当前 revision → 与 expected 比对
// （force=false 且不一致则 ErrFileModified）→ 写同目录临时文件 → fsync → 原子 rename →
// stat 计算新 revision。content 原样写入，不做行尾 / 编码转换。
func (b *Local) WriteText(_ context.Context, p string, content []byte, expected model.FileRevision, force bool) (*model.SaveContentResult, error) {
	cleaned := util.CleanAPIPath(p)
	if cleaned == "/" {
		return nil, storage.ErrNotFile
	}
	local, err := util.SafeResolve(b.rootReal, cleaned)
	if err != nil {
		return nil, mapErr(err)
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, mapErr(err)
	}
	if fi.IsDir() {
		return nil, storage.ErrNotFile
	}
	if !fi.Mode().IsRegular() {
		return nil, storage.ErrNotFile
	}
	if !isWritable(fi.Mode()) {
		return nil, storage.ErrReadonly
	}

	// 乐观锁：重新读取当前内容计算 revision，与前端带回的 expected 比对。
	current, err := os.ReadFile(local)
	if err != nil {
		return nil, mapErr(err)
	}
	if !force {
		if expected.Token == "" {
			return nil, storage.ErrInvalidRev
		}
		if revisionOf(current).Token != expected.Token {
			return nil, storage.ErrFileModified
		}
	}

	// 写同目录临时文件 → fsync → 原子 rename，保持文件原权限位。
	if err := atomicWrite(local, content, fi.Mode().Perm()); err != nil {
		return nil, mapErr(err)
	}

	newFi, err := os.Stat(local)
	if err != nil {
		return nil, mapErr(err)
	}
	return &model.SaveContentResult{
		Path:     cleaned,
		Size:     newFi.Size(),
		ModTime:  newFi.ModTime(),
		Revision: revisionOf(content),
	}, nil
}

// atomicWrite 在 target 同目录写临时文件，fsync 后原子 rename 覆盖，避免写中途损坏既有文件。
func atomicWrite(target string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".flist-edit-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if perm != 0 {
		_ = os.Chmod(tmpName, perm)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return err
	}
	return nil
}

// revisionOf 由内容字节计算强校验 revision（sha256:<hex>，weak=false）。
func revisionOf(data []byte) model.FileRevision {
	sum := sha256.Sum256(data)
	return model.FileRevision{Token: "sha256:" + hex.EncodeToString(sum[:]), Weak: false}
}

// isEditableText 判断内容是否可作为文本编辑：已知文本扩展名直接放行（仍需通过 NUL 嗅探），
// 未知扩展名则依赖内容嗅探（前 sniffBytes 字节无 NUL）。
func isEditableText(name string, data []byte) bool {
	sample := data
	if len(sample) > textSniffBytes {
		sample = sample[:textSniffBytes]
	}
	if !util.SniffText(sample) {
		return false // 含 NUL → 二进制
	}
	kind := util.DetectKind(name)
	if kind == util.KindImage || kind == util.KindVideo || kind == util.KindAudio {
		return false
	}
	if util.IsTextExt(name) {
		return true
	}
	// 未知类型：内容无 NUL 即视为文本（空文件也视为可编辑文本）。
	return kind == util.KindUnknown
}

// textSniffBytes 是文本嗅探采样字节数（与 service 预览嗅探口径一致）。
const textSniffBytes = 512

// decodeEncoding 识别 UTF-8 BOM 并返回编码标记与去 BOM 后的字符串内容。
// 注意：revision 基于原始字节（含 BOM）计算，保证保存时按原样比对。
func decodeEncoding(data []byte) (encoding, content string) {
	if bytes.HasPrefix(data, utf8BOM) {
		return model.EncodingUTF8BOM, string(data[len(utf8BOM):])
	}
	return model.EncodingUTF8, string(data)
}

// detectLineEnding 探测内容行尾风格：仅 LF、仅 CRLF、混合或无换行。
func detectLineEnding(content string) string {
	hasCRLF := strings.Contains(content, "\r\n")
	// 去掉 CRLF 后若仍有裸 LF，则存在 LF。
	withoutCRLF := strings.ReplaceAll(content, "\r\n", "")
	hasLF := strings.Contains(withoutCRLF, "\n")

	switch {
	case hasCRLF && hasLF:
		return model.LineEndingMixed
	case hasCRLF:
		return model.LineEndingCRLF
	case hasLF:
		return model.LineEndingLF
	default:
		return model.LineEndingNone
	}
}

// detectMIME 由扩展名推导 MIME，缺省回落 text/plain; charset=utf-8。
func detectMIME(name string) string {
	if ct := mime.TypeByExtension(strings.ToLower(path.Ext(name))); ct != "" {
		return ct
	}
	return "text/plain; charset=utf-8"
}

// isWritable 依据 owner 写位粗略判断文件是否可写（本地单用户语义，足够给前端提示）。
func isWritable(mode os.FileMode) bool {
	return mode.Perm()&0o200 != 0
}
