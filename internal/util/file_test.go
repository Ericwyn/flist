package util

import (
	"io/fs"
	"testing"
)

func TestFormatMode(t *testing.T) {
	cases := map[fs.FileMode]string{
		0o644:                  "0644",
		0o755:                  "0755",
		0o600:                  "0600",
		0o777:                  "0777",
		0o000:                  "0000",
		fs.ModeDir | 0o755:     "0755", // 类型位被忽略
		fs.ModeSymlink | 0o777: "0777",
	}
	for mode, want := range cases {
		if got := FormatMode(mode); got != want {
			t.Errorf("FormatMode(%v) = %q, want %q", mode, got, want)
		}
	}
}

func TestDetectKind(t *testing.T) {
	cases := map[string]Kind{
		"a.txt":     KindText,
		"README.md": KindText,
		"photo.JPG": KindImage,
		"clip.mp4":  KindVideo,
		"song.flac": KindAudio,
		"data.bin":  KindUnknown,
		"noext":     KindUnknown,
	}
	for name, want := range cases {
		if got := DetectKind(name); got != want {
			t.Errorf("DetectKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIsTextExt(t *testing.T) {
	if !IsTextExt("config.yaml") {
		t.Error("yaml should be text")
	}
	if IsTextExt("image.png") {
		t.Error("png should not be text")
	}
}

func TestSniffText(t *testing.T) {
	if !SniffText([]byte("hello world\nplain text")) {
		t.Error("plain text should be detected as text")
	}
	if SniffText([]byte{0x00, 0x01, 0x02}) {
		t.Error("content with NUL should be binary")
	}
	if !SniffText([]byte{}) {
		t.Error("empty should be text")
	}
}

func TestIsHidden_Linux(t *testing.T) {
	// 该测试仅在 linux 下有意义（hidden_linux.go），其它平台行为可能不同。
	if !IsHidden(".bashrc", nil) {
		t.Error("dotfile should be hidden")
	}
	if IsHidden("visible.txt", nil) {
		t.Error("normal file should not be hidden")
	}
}
