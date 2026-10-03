package extractor

import "encoding/json"

// redditGalleryOptions restricts extraction to native media from one public post.
func redditGalleryOptions(request Request, proxyURL, ffmpegPath string) []string {
	raw := redditDownloaderOptions(request, proxyURL, ffmpegPath, false)
	return []string{
		"-o", "extractor.reddit.api=rest", "-o", "extractor.reddit.comments=0",
		"-o", "extractor.reddit.recursion=0", "-o", "extractor.reddit.selftext=false",
		"-o", "extractor.reddit.previews=false", "-o", "extractor.reddit.image-filter=not _url.startswith('https://preview.redd.it/')", "-o", "extractor.reddit.videos=dash",
		"-o", "downloader.ytdl.module=yt_dlp", "-o", "downloader.ytdl.raw-options=" + raw,
	}
}

func redditDownloaderOptions(request Request, proxyURL, ffmpegPath string, flat bool) string {
	opts := map[string]any{
		"proxy": proxyURL, "ffmpeg_location": ffmpegPath, "format": ytdlpFormat,
		"merge_output_format": "mp4", "max_filesize": request.MaxBytes,
		"retries": 3, "fragment_retries": 3, "file_access_retries": 1, "socket_timeout": 30,
		"concurrent_fragment_downloads": 1, "ignoreerrors": false, "skip_unavailable_fragments": false, "noplaylist": true,
		"writethumbnail": false, "writeinfojson": false, "writesubtitles": false,
		"writeautomaticsub": false, "getcomments": false, "cachedir": false,
		"remote_components": []string{}, "enable_file_urls": false, "noprogress": true,
		"nocheckcertificate": false, "hls_prefer_native": true,
	}
	if flat {
		opts["omdi_max_items"] = request.MaxItems
		opts["extract_flat"] = true
		opts["allowed_extractors"] = []string{"reddit"}
		opts["outtmpl"] = map[string]string{"default": "item-000.%(ext)s"}
		opts["paths"] = map[string]string{"home": request.WorkDir}
	}
	raw, _ := json.Marshal(opts) // Every value has a known JSON representation.
	return string(raw)
}
