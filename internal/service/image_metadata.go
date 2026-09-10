package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"path"
	"strconv"
	"strings"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

const maxMetadataScanBytes = 8 << 20

// ImageMetadata 从原文件读取图片尺寸、格式和常用 EXIF 拍摄信息。
// 无 EXIF 或标准库无法解码的图片格式仍返回文件级信息，不影响图片本身的在线预览。
func (s *FileService) ImageMetadata(ctx context.Context, apiPath string) (*model.ImageMetadata, error) {
	cleaned := util.CleanAPIPath(apiPath)
	f, info, err := s.backend.Open(ctx, cleaned)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if util.DetectKind(info.Name) != util.KindImage {
		return nil, storage.ErrUnsupportedMedia
	}

	result := &model.ImageMetadata{
		Size:    info.Size,
		ModTime: info.ModTime,
		MIME:    mime.TypeByExtension(strings.ToLower(path.Ext(info.Name))),
		EXIF:    []model.ImageMetadataField{},
	}

	data, readErr := io.ReadAll(io.LimitReader(f, maxMetadataScanBytes))
	if readErr != nil {
		return result, nil
	}
	if cfg, format, decodeErr := image.DecodeConfig(bytes.NewReader(data)); decodeErr == nil {
		result.Format = strings.ToUpper(format)
		result.Width = cfg.Width
		result.Height = cfg.Height
		result.ColorModel = colorModelName(cfg.ColorModel)
		if result.MIME == "" {
			result.MIME = "image/" + format
		}
	}

	result.EXIF = parseJPEGExif(data)
	if result.Format == "" {
		result.Format = strings.ToUpper(strings.TrimPrefix(path.Ext(info.Name), "."))
	}
	return result, nil
}

func colorModelName(m color.Model) string {
	switch m {
	case color.RGBAModel:
		return "RGBA"
	case color.RGBA64Model:
		return "RGBA64"
	case color.NRGBAModel:
		return "NRGBA"
	case color.NRGBA64Model:
		return "NRGBA64"
	case color.GrayModel:
		return "Gray"
	case color.Gray16Model:
		return "Gray16"
	case color.YCbCrModel:
		return "YCbCr"
	case color.CMYKModel:
		return "CMYK"
	}
	if _, ok := m.(color.Palette); ok {
		return "Palette"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", m), "color.")
}

type exifTag struct {
	key   string
	label string
}

var exifTags = map[uint16]exifTag{
	0x010f: {"make", "设备厂商"},
	0x0110: {"model", "设备型号"},
	0x0112: {"orientation", "方向"},
	0x011a: {"x_resolution", "水平分辨率"},
	0x011b: {"y_resolution", "垂直分辨率"},
	0x0128: {"resolution_unit", "分辨率单位"},
	0x0131: {"software", "处理软件"},
	0x0132: {"modified_at", "修改时间"},
	0x829a: {"exposure_time", "曝光时间"},
	0x829d: {"f_number", "光圈"},
	0x8822: {"exposure_program", "曝光程序"},
	0x8827: {"iso", "ISO"},
	0x9003: {"taken_at", "拍摄时间"},
	0x9004: {"digitized_at", "数字化时间"},
	0x9204: {"exposure_bias", "曝光补偿"},
	0x9207: {"metering_mode", "测光模式"},
	0x9209: {"flash", "闪光灯"},
	0x920a: {"focal_length", "焦距"},
	0xa001: {"color_space", "色彩空间"},
	0xa002: {"pixel_width", "EXIF 宽度"},
	0xa003: {"pixel_height", "EXIF 高度"},
	0xa405: {"focal_length_35mm", "35mm 等效焦距"},
	0xa433: {"lens_make", "镜头厂商"},
	0xa434: {"lens_model", "镜头型号"},
}

type tiffReader struct {
	data  []byte
	order binary.ByteOrder
}

// parseJPEGExif 查找 JPEG APP1 中的 TIFF EXIF。未知或损坏字段会被忽略，绝不阻断预览。
func parseJPEGExif(data []byte) []model.ImageMetadataField {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return []model.ImageMetadataField{}
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			pos++
			continue
		}
		marker := data[pos+1]
		pos += 2
		if marker == 0xd9 || marker == 0xda {
			break
		}
		if marker == 0x00 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if pos+2 > len(data) {
			break
		}
		segmentLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segmentLen < 2 || pos+segmentLen > len(data) {
			break
		}
		payload := data[pos+2 : pos+segmentLen]
		if marker == 0xe1 && len(payload) >= 14 && bytes.Equal(payload[:6], []byte("Exif\x00\x00")) {
			return parseTIFF(payload[6:])
		}
		pos += segmentLen
	}
	return []model.ImageMetadataField{}
}

func parseTIFF(data []byte) []model.ImageMetadataField {
	if len(data) < 8 {
		return []model.ImageMetadataField{}
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return []model.ImageMetadataField{}
	}
	if order.Uint16(data[2:4]) != 42 {
		return []model.ImageMetadataField{}
	}
	r := tiffReader{data: data, order: order}
	ifd0 := order.Uint32(data[4:8])
	fields, pointers := r.readIFD(ifd0)
	if exifOffset := pointers[0x8769]; exifOffset != 0 {
		exifFields, _ := r.readIFD(exifOffset)
		fields = append(fields, exifFields...)
	}
	if gpsOffset := pointers[0x8825]; gpsOffset != 0 {
		fields = append(fields, r.readGPS(gpsOffset)...)
	}
	return fields
}

func (r tiffReader) readIFD(offset uint32) ([]model.ImageMetadataField, map[uint16]uint32) {
	fields := []model.ImageMetadataField{}
	pointers := map[uint16]uint32{}
	if int(offset)+2 > len(r.data) {
		return fields, pointers
	}
	count := int(r.order.Uint16(r.data[offset : offset+2]))
	if count > 512 {
		count = 512
	}
	base := int(offset) + 2
	for i := 0; i < count; i++ {
		pos := base + i*12
		if pos+12 > len(r.data) {
			break
		}
		tag := r.order.Uint16(r.data[pos : pos+2])
		typ := r.order.Uint16(r.data[pos+2 : pos+4])
		n := r.order.Uint32(r.data[pos+4 : pos+8])
		if tag == 0x8769 || tag == 0x8825 {
			if typ == 4 && n == 1 {
				pointers[tag] = r.order.Uint32(r.data[pos+8 : pos+12])
			}
			continue
		}
		info, ok := exifTags[tag]
		if !ok {
			continue
		}
		value, ok := r.value(pos, typ, n)
		if !ok || value == "" {
			continue
		}
		value = prettyExifValue(tag, value)
		fields = append(fields, model.ImageMetadataField{Key: info.key, Label: info.label, Value: value})
	}
	return fields, pointers
}

func (r tiffReader) value(entryPos int, typ uint16, count uint32) (string, bool) {
	typeSize := map[uint16]uint64{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}[typ]
	if typeSize == 0 || count == 0 || uint64(count) > uint64(len(r.data)) || typeSize*uint64(count) > uint64(len(r.data)) {
		return "", false
	}
	size := int(typeSize * uint64(count))
	start := entryPos + 8
	if size > 4 {
		offset := int(r.order.Uint32(r.data[entryPos+8 : entryPos+12]))
		if offset < 0 || offset+size > len(r.data) {
			return "", false
		}
		start = offset
	}
	b := r.data[start : start+size]
	values := make([]string, 0, count)
	switch typ {
	case 1, 7:
		for _, v := range b {
			values = append(values, strconv.Itoa(int(v)))
		}
	case 2:
		return strings.TrimSpace(strings.TrimRight(string(b), "\x00")), true
	case 3:
		for i := uint32(0); i < count; i++ {
			values = append(values, strconv.FormatUint(uint64(r.order.Uint16(b[i*2:i*2+2])), 10))
		}
	case 4:
		for i := uint32(0); i < count; i++ {
			values = append(values, strconv.FormatUint(uint64(r.order.Uint32(b[i*4:i*4+4])), 10))
		}
	case 9:
		for i := uint32(0); i < count; i++ {
			values = append(values, strconv.FormatInt(int64(int32(r.order.Uint32(b[i*4:i*4+4]))), 10))
		}
	case 5, 10:
		for i := uint32(0); i < count; i++ {
			pos := i * 8
			numRaw := r.order.Uint32(b[pos : pos+4])
			denRaw := r.order.Uint32(b[pos+4 : pos+8])
			if denRaw == 0 {
				continue
			}
			var num, den int64 = int64(numRaw), int64(denRaw)
			if typ == 10 {
				num, den = int64(int32(numRaw)), int64(int32(denRaw))
			}
			values = append(values, fmt.Sprintf("%d/%d", num, den))
		}
	}
	return strings.Join(values, ", "), len(values) > 0
}

func prettyExifValue(tag uint16, value string) string {
	firstNumber := func() int {
		n, _ := strconv.Atoi(strings.Split(value, ",")[0])
		return n
	}
	switch tag {
	case 0x0112:
		if label := map[int]string{1: "正常", 2: "水平翻转", 3: "旋转 180°", 4: "垂直翻转", 5: "转置", 6: "顺时针 90°", 7: "横向转置", 8: "逆时针 90°"}[firstNumber()]; label != "" {
			return label
		}
	case 0x0128:
		if label := map[int]string{1: "无单位", 2: "英寸", 3: "厘米"}[firstNumber()]; label != "" {
			return label
		}
	case 0x829a:
		return value + " 秒"
	case 0x829d:
		return "f/" + decimalRational(value, 1)
	case 0x8822:
		if label := map[int]string{0: "未定义", 1: "手动", 2: "标准程序", 3: "光圈优先", 4: "快门优先", 5: "创意程序", 6: "动作程序", 7: "人像", 8: "风景"}[firstNumber()]; label != "" {
			return label
		}
	case 0x9204:
		return decimalRational(value, 2) + " EV"
	case 0x9207:
		if label := map[int]string{0: "未知", 1: "平均", 2: "中央重点", 3: "点测光", 4: "多点", 5: "多区", 6: "局部"}[firstNumber()]; label != "" {
			return label
		}
	case 0x9209:
		if firstNumber()&1 == 1 {
			return "已闪光"
		}
		return "未闪光"
	case 0x920a:
		return decimalRational(value, 1) + " mm"
	case 0xa405:
		return value + " mm"
	case 0xa001:
		if firstNumber() == 1 {
			return "sRGB"
		}
	}
	if value == "" {
		return "未知"
	}
	return value
}

func decimalRational(value string, precision int) string {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return value
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(strings.Fields(parts[1])[0], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return value
	}
	return strconv.FormatFloat(num/den, 'f', precision, 64)
}

func (r tiffReader) readGPS(offset uint32) []model.ImageMetadataField {
	if int(offset)+2 > len(r.data) {
		return nil
	}
	count := int(r.order.Uint16(r.data[offset : offset+2]))
	if count > 64 {
		count = 64
	}
	values := map[uint16]string{}
	base := int(offset) + 2
	for i := 0; i < count; i++ {
		pos := base + i*12
		if pos+12 > len(r.data) {
			break
		}
		tag := r.order.Uint16(r.data[pos : pos+2])
		value, ok := r.value(pos, r.order.Uint16(r.data[pos+2:pos+4]), r.order.Uint32(r.data[pos+4:pos+8]))
		if ok {
			values[tag] = value
		}
	}
	fields := []model.ImageMetadataField{}
	if value := formatGPS(values[2], values[1]); value != "" {
		fields = append(fields, model.ImageMetadataField{Key: "gps_latitude", Label: "GPS 纬度", Value: value})
	}
	if value := formatGPS(values[4], values[3]); value != "" {
		fields = append(fields, model.ImageMetadataField{Key: "gps_longitude", Label: "GPS 经度", Value: value})
	}
	if altitude := values[6]; altitude != "" {
		prefix := ""
		if values[5] == "1" {
			prefix = "-"
		}
		fields = append(fields, model.ImageMetadataField{Key: "gps_altitude", Label: "GPS 海拔", Value: prefix + decimalRational(altitude, 1) + " m"})
	}
	return fields
}

func formatGPS(value, ref string) string {
	parts := strings.Split(value, ", ")
	if len(parts) != 3 {
		return ""
	}
	return fmt.Sprintf("%s° %s′ %s″ %s", decimalRational(parts[0], 0), decimalRational(parts[1], 0), decimalRational(parts[2], 2), ref)
}
