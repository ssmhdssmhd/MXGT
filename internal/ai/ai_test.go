package ai

import (
	"context"
	"testing"
)

const sampleM3U8 = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:10.000,
https://cdn.example.com/seg0.ts
#EXTINF:10.000,
https://cdn.example.com/seg1.ts
#EXTINF:3.000,
https://cdn.example.com/ad_short.ts
#EXT-X-ENDLIST
`

func TestParseM3U8(t *testing.T) {
	pl, err := ParseM3U8(context.Background(), sampleM3U8)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(pl.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(pl.Segments))
	}
	if pl.Segments[0].Duration != 10.0 || pl.Segments[2].Duration != 3.0 {
		t.Fatalf("duration parse wrong: %+v", pl.Segments)
	}
	if pl.Segments[2].URL != "https://cdn.example.com/ad_short.ts" {
		t.Fatalf("url wrong: %s", pl.Segments[2].URL)
	}
	if pl.TargetDur != 10.0 {
		t.Fatalf("target duration wrong: %v", pl.TargetDur)
	}
}

func TestHeuristic(t *testing.T) {
	if v := ClassifyHeuristic(3.0, 10.0); v != VerdictAd {
		t.Fatalf("short segment should be ad, got %s", v)
	}
	if v := ClassifyHeuristic(9.0, 10.0); v != VerdictUnknown {
		t.Fatalf("normal segment should be unknown, got %s", v)
	}
	if !Bad(VerdictAd) || Bad(VerdictNormal) {
		t.Fatalf("Bad logic wrong")
	}
}

func TestCleanM3U8(t *testing.T) {
	segs := []Segment{
		{Seq: 0, Duration: 10, URL: "https://a/0.ts", MD5: "aaa"},
		{Seq: 1, Duration: 10, URL: "https://a/1.ts", MD5: "bbb"},
	}
	clean := GenerateCleanM3U8(segs, map[string]bool{"aaa": true}, 10.0)
	if contains(clean, "0.ts") {
		t.Fatalf("clean m3u8 should drop 0.ts:\n%s", clean)
	}
	if !contains(clean, "1.ts") {
		t.Fatalf("clean m3u8 should keep 1.ts:\n%s", clean)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}