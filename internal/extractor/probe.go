package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

// Inspection is the trusted classification of one local media file.
type Inspection struct {
	Name, MediaType, Format, VideoCodec, AudioCodec string
	NeedsRemux                                      bool
}

// Probe inspects local media with ffprobe.
type Probe struct {
	path   string
	runner *Runner
}

// NewProbe constructs an ffprobe adapter.
func NewProbe(path string, runner *Runner) *Probe { return &Probe{path: path, runner: runner} }

type probeDocument struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Tags       struct {
			MajorBrand string `json:"major_brand"`
		} `json:"tags"`
	} `json:"format"`
}

// Inspect validates a local entry and returns its media classification.
func (p *Probe) Inspect(ctx context.Context, workDir, name string) (Inspection, error) {
	if !safeWorkDir(workDir) || !plainName(name) {
		return Inspection{}, errors.New("extractor: invalid local media entry")
	}
	path := filepath.Join(workDir, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Inspection{}, errors.New("extractor: invalid local media entry")
	}
	// ffprobe has no -nostdin option; the runner already supplies empty standard input.
	result, err := p.runner.Run(ctx, Command{Path: p.path, Dir: workDir, StdoutLimit: 256 << 10, StderrLimit: 64 << 10, Args: []string{
		"-v", "error", "-protocol_whitelist", "file", "-show_format", "-show_streams", "-of", "json", path,
	}})
	if err != nil {
		return Inspection{}, toolFailure(ctx, "ffprobe", job.DetailProcessingFailed, result, err)
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return Inspection{}, ErrExtractionFailed
	}
	var doc probeDocument
	decoder := json.NewDecoder(bytes.NewReader(result.Stdout))
	if decoder.Decode(&doc) != nil {
		return Inspection{}, ErrExtractionFailed
	}
	var trailing any
	if decoder.Decode(&trailing) == nil {
		return Inspection{}, ErrExtractionFailed
	}
	inspection, ok := classifyProbe(name, doc)
	if !ok {
		return Inspection{}, ErrExtractionFailed
	}
	return inspection, nil
}

func safeWorkDir(dir string) bool { return filepath.IsAbs(dir) && filepath.Clean(dir) == dir }

func plainName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !filepath.IsAbs(name) && !strings.ContainsAny(name, `/\\`)
}

func classifyProbe(name string, doc probeDocument) (Inspection, bool) {
	formats := make(map[string]bool)
	for _, f := range strings.Split(doc.Format.FormatName, ",") {
		formats[f] = true
	}
	var video, audio string
	for _, stream := range doc.Streams {
		switch stream.CodecType {
		case "video":
			if video != "" || stream.CodecName == "" {
				return Inspection{}, false
			}
			video = stream.CodecName
		case "audio":
			if audio != "" || stream.CodecName == "" {
				return Inspection{}, false
			}
			audio = stream.CodecName
		default:
			return Inspection{}, false
		}
	}
	if video == "" && audio == "" {
		return Inspection{}, false
	}
	i := Inspection{Name: name, Format: doc.Format.FormatName, VideoCodec: video, AudioCodec: audio}
	mov := formats["mov"] || formats["mp4"] || formats["m4a"] || formats["3gp"] || formats["3g2"] || formats["mj2"]
	switch {
	case mov && video == "h264" && (audio == "" || audio == "aac"):
		if doc.Format.Tags.MajorBrand == "qt  " {
			i.MediaType = "video/quicktime"
		} else {
			i.MediaType = "video/mp4"
		}
	case mov && video == "" && audio == "aac":
		i.MediaType = "audio/mp4"
	case formats["mp3"] && video == "" && audio == "mp3":
		i.MediaType = "audio/mpeg"
	case formats["image2"] && video == "mjpeg" && audio == "", formats["jpeg_pipe"] && video == "mjpeg" && audio == "":
		i.MediaType = "image/jpeg"
	case formats["image2"] && video == "png" && audio == "", formats["png_pipe"] && video == "png" && audio == "":
		i.MediaType = "image/png"
	case formats["image2"] && video == "webp" && audio == "", formats["webp_pipe"] && video == "webp" && audio == "":
		i.MediaType = "image/webp"
	case (formats["gif"] || formats["image2"]) && video == "gif" && audio == "":
		i.MediaType = "image/gif"
	case formats["mpegts"] && video == "h264" && (audio == "" || audio == "aac"):
		i.MediaType, i.NeedsRemux = "video/mp2t", true
	default:
		return Inspection{}, false
	}
	return i, true
}
