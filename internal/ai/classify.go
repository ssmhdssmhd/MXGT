package ai

import (
	"strconv"
	"strings"
)

// 判定常量
const (
	VerdictNormal    = "normal"    // 正常内容
	VerdictAd        = "ad"        // 广告
	VerdictSubtitle  = "subtitle"  // 字幕段
	VerdictInterlude = "interlude" // 插播（片头/预告）
	VerdictWatermark = "watermark" // 台标/水印
	VerdictUnknown   = "unknown"
)

// Bad 判定是否算"需要剔除"的垃圾片段（广告/字幕/插播/水印）
func Bad(v string) bool {
	switch v {
	case VerdictAd, VerdictSubtitle, VerdictInterlude, VerdictWatermark:
		return true
	}
	return false
}

// ClassifyHeuristic 纯规则启发式判定（不依赖 AI）。
// dur: 该分片时长；avgDur: 整体平均时长。时长异常短 → 疑似广告/插播。
func ClassifyHeuristic(dur, avgDur float64) string {
	if avgDur <= 0 {
		return VerdictUnknown
	}
	if dur > 0 && dur < avgDur*0.5 && avgDur-dur >= 3 {
		return VerdictAd
	}
	return VerdictUnknown
}

// IsDirtyName 根据分片/来源名称关键词辅助判断（可选）
func IsDirtyName(name string) bool {
	s := strings.ToLower(name)
	for _, k := range []string{"ad", "adv", "_ad_", "广告", "片头", "预告", "interlude", "promo"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// GenerateCleanM3U8 生成去广告后的 m3u8 文本。
// segments 为原始分片，skipMD5 为需剔除分片的 MD5 集合。
func GenerateCleanM3U8(segments []Segment, skipMD5 map[string]bool, targetDur float64) string {
	if targetDur <= 0 {
		targetDur = 10
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-TARGETDURATION:" + formatF(targetDur) + "\n")
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	for _, s := range segments {
		if skipMD5[s.MD5] {
			continue
		}
		d := s.Duration
		if d <= 0 {
			d = targetDur
		}
		b.WriteString("#EXTINF:" + formatF(d) + ",\n")
		b.WriteString(s.URL + "\n")
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// formatF 将浮点数格式化为紧凑十进制
func formatF(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}