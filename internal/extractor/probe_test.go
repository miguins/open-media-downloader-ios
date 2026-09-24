package extractor

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestProbeArguments(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	dir := t.TempDir()
	regular(t, dir, "media.mp4")
	tool := testTool(t, `printf '%s\n' "$@" > '`+argsFile+`'
printf '%s' '`+probeJSON("mp4", "h264", "aac")+`'`)
	if _, err := NewProbe(tool, NewRunner()).Inspect(t.Context(), dir, "media.mp4"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile) //nolint:gosec // Path is under the test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	// ffprobe rejects -nostdin, which only ffmpeg accepts.
	want := "-v\nerror\n-protocol_whitelist\nfile\n-show_format\n-show_streams\n-of\njson\n" + filepath.Join(dir, "media.mp4") + "\n"
	if string(raw) != want {
		t.Fatalf("arguments = %q, want %q", raw, want)
	}
}
