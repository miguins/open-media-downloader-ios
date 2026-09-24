package extractor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type MediaTools struct {
	probe      *Probe
	ffmpegPath string
	runner     *Runner
}

func NewMediaTools(ffprobePath, ffmpegPath string, runner *Runner) *MediaTools {
	return &MediaTools{probe: NewProbe(ffprobePath, runner), ffmpegPath: ffmpegPath, runner: runner}
}

func (m *MediaTools) Finalize(ctx context.Context, workDir string, names []string) ([]File, error) {
	if len(names) == 0 || !safeWorkDir(workDir) {
		return nil, ErrExtractionFailed
	}
	seen := make(map[string]bool, len(names))
	files := make([]File, 0, len(names))
	for index, name := range names {
		if seen[name] || !plainName(name) {
			return nil, ErrExtractionFailed
		}
		seen[name] = true
		inspection, err := m.probe.Inspect(ctx, workDir, name)
		if err != nil {
			return nil, err
		}
		if inspection.NeedsRemux {
			inspection, err = m.remux(ctx, workDir, inspection, index+1)
			if err != nil {
				return nil, err
			}
		}
		files = append(files, File{Name: inspection.Name, MediaType: inspection.MediaType})
	}
	return files, nil
}

func (m *MediaTools) remux(ctx context.Context, workDir string, input Inspection, index int) (Inspection, error) {
	temporary := fmt.Sprintf(".omdi-remux-%03d.mp4", index)
	destination := fmt.Sprintf("media-%03d.mp4", index)
	temporaryPath := filepath.Join(workDir, temporary)
	destinationPath := filepath.Join(workDir, destination)
	if _, err := os.Lstat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		return Inspection{}, errors.New("extractor: remux output unavailable")
	}
	if _, err := os.Lstat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		return Inspection{}, errors.New("extractor: remux destination unavailable")
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	result, err := m.runner.Run(ctx, Command{Path: m.ffmpegPath, Dir: workDir, StdoutLimit: 64 << 10, StderrLimit: 64 << 10, Args: []string{
		"-v", "error", "-nostdin", "-n", "-protocol_whitelist", "file", "-i", filepath.Join(workDir, input.Name),
		"-map", "0:v:0?", "-map", "0:a:0?", "-c", "copy", "-movflags", "+faststart", temporaryPath,
	}})
	_ = result
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Inspection{}, err
		}
		if errors.Is(err, ErrCommandExit) || errors.Is(err, ErrOutputLimit) {
			return Inspection{}, ErrExtractionFailed
		}
		return Inspection{}, err
	}
	output, err := m.probe.Inspect(ctx, workDir, temporary)
	if err != nil {
		return Inspection{}, err
	}
	if output.MediaType != "video/mp4" || output.NeedsRemux {
		return Inspection{}, ErrExtractionFailed
	}
	if err := os.Remove(filepath.Join(workDir, input.Name)); err != nil {
		return Inspection{}, errors.New("extractor: replace remux input")
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return Inspection{}, errors.New("extractor: install remux output")
	}
	cleanup = false
	output.Name = destination
	return output, nil
}
