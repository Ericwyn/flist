package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImageMetadataPNG(t *testing.T) {
	svc, root := setupTestRoot(t)
	img := image.NewNRGBA(image.Rect(0, 0, 37, 19))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.png"), encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	metadata, err := svc.ImageMetadata(context.Background(), "/sample.png")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Format != "PNG" || metadata.MIME != "image/png" {
		t.Fatalf("unexpected format: %+v", metadata)
	}
	if metadata.Width != 37 || metadata.Height != 19 {
		t.Fatalf("unexpected dimensions: %dx%d", metadata.Width, metadata.Height)
	}
	if metadata.Size != int64(encoded.Len()) || len(metadata.EXIF) != 0 {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}

func TestImageMetadataRejectsNonImage(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "note.txt", "hello")
	if _, err := svc.ImageMetadata(context.Background(), "/note.txt"); err == nil {
		t.Fatal("expected non-image to be rejected")
	}
}

func TestParseJPEGExif(t *testing.T) {
	// 构造一个小端 TIFF：IFD0 包含厂商、型号、方向和 ExifIFD 指针；
	// ExifIFD 包含曝光时间与 ISO。偏移均以 TIFF 头起点计算。
	tiff := make([]byte, 112)
	copy(tiff[0:2], "II")
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 4)
	putIFDEntry(tiff[10:22], 0x010f, 2, 6, 62) // Canon\0
	putIFDEntry(tiff[22:34], 0x0110, 2, 6, 68) // EOS R\0
	putIFDEntry(tiff[34:46], 0x0112, 3, 1, 6)
	putIFDEntry(tiff[46:58], 0x8769, 4, 1, 74)
	copy(tiff[62:68], "Canon\x00")
	copy(tiff[68:74], "EOS R\x00")
	binary.LittleEndian.PutUint16(tiff[74:76], 2)
	putIFDEntry(tiff[76:88], 0x829a, 5, 1, 104)
	putIFDEntry(tiff[88:100], 0x8827, 3, 1, 400)
	binary.LittleEndian.PutUint32(tiff[104:108], 1)
	binary.LittleEndian.PutUint32(tiff[108:112], 125)

	payload := append([]byte("Exif\x00\x00"), tiff...)
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(jpeg[4:6], uint16(len(payload)+2))
	jpeg = append(jpeg, payload...)
	jpeg = append(jpeg, 0xff, 0xd9)

	fields := parseJPEGExif(jpeg)
	want := map[string]string{
		"make":          "Canon",
		"model":         "EOS R",
		"orientation":   "顺时针 90°",
		"exposure_time": "1/125 秒",
		"iso":           "400",
	}
	if len(fields) != len(want) {
		t.Fatalf("expected %d fields, got %+v", len(want), fields)
	}
	for _, field := range fields {
		if want[field.Key] != field.Value {
			t.Errorf("unexpected %s: %q", field.Key, field.Value)
		}
	}
}

func putIFDEntry(dst []byte, tag, typ uint16, count, value uint32) {
	binary.LittleEndian.PutUint16(dst[0:2], tag)
	binary.LittleEndian.PutUint16(dst[2:4], typ)
	binary.LittleEndian.PutUint32(dst[4:8], count)
	if typ == 3 && count == 1 {
		binary.LittleEndian.PutUint16(dst[8:10], uint16(value))
		return
	}
	binary.LittleEndian.PutUint32(dst[8:12], value)
}
