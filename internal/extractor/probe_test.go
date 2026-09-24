package extractor

import "testing"

func TestProbeClassification(t *testing.T) {
	var d probeDocument
	d.Format.FormatName = "mov,mp4,m4a,3gp,3g2,mj2"
	d.Streams = append(d.Streams, struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	}{"video", "h264"})
	i, ok := classifyProbe("media.bin", d)
	if !ok || i.MediaType != "video/mp4" {
		t.Fatalf("classification = %#v, %v", i, ok)
	}
}

func TestProbeRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", `/tmp/a`} {
		if plainName(name) {
			t.Errorf("plainName(%q) = true", name)
		}
	}
}
