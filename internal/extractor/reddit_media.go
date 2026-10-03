package extractor

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

// This fixed program runs inside the pinned media-tool virtual environment.
// Extraction remains unprocessed until native media data is validated, so URL
// and playlist results cannot cause linked-post or external-site recursion.
const redditMediaProgram = `import html, json, os, re, sys
from urllib.parse import urlsplit, urlunsplit
import yt_dlp
from yt_dlp.extractor.reddit import RedditIE as BaseRedditIE
from yt_dlp.globals import plugin_dirs
from yt_dlp.networking import Request
from yt_dlp.networking.exceptions import HTTPError, RequestError
from yt_dlp.utils import DownloadError
plugin_dirs.value = []

def fail(message):
    print("ERROR: " + message, file=sys.stderr)
    sys.exit(4)

class RedditIE(BaseRedditIE):
    post = None
    def _download_json(self, url, video_id, *args, **kwargs):
        data = super()._download_json(url, video_id, *args, **kwargs)
        if isinstance(data, list) and data:
            children = data[0].get("data", {}).get("children", [])
            if children and isinstance(children[0].get("data"), dict):
                post = children[0]["data"]
                if post.get("id") != video_id:
                    fail("Invalid native media metadata")
                self.post = post
        return data

def original(raw, gallery=False):
    if not isinstance(raw, str):
        fail("Invalid native media metadata")
    parsed = urlsplit(html.unescape(raw))
    host = parsed.netloc.lower()
    if gallery and host == "preview.redd.it":
        parsed = parsed._replace(netloc="i.redd.it", query="")
        host = "i.redd.it"
    extensions = "jpe?g|png|gif|webp|mp4" if gallery else "jpe?g|png|gif|webp"
    if parsed.scheme != "https" or host != "i.redd.it" or parsed.fragment or not re.fullmatch(r"/[A-Za-z0-9_-]+\.(?:" + extensions + ")", parsed.path, re.I):
        fail("Invalid native media metadata")
    return urlunsplit(parsed), parsed.path.rsplit(".", 1)[1].lower()

def main():
    options = json.loads(sys.argv[1])
    max_items = options.pop("omdi_max_items")
    options["ignore_no_formats_error"] = True
    with yt_dlp.YoutubeDL(options) as downloader:
        extractor = RedditIE()
        downloader.add_info_extractor(extractor)
        info = downloader.extract_info(sys.argv[2], download=False, process=False)
        if info and info.get("_type", "video") == "video" and info.get("formats"):
            downloader.params["ignore_no_formats_error"] = False
            downloader.process_ie_result(info, download=True)
        else:
            post = extractor.post
            if not post:
                fail("Invalid native media metadata")
            gallery = post.get("gallery_data")
            if gallery is not None:
                entries = gallery.get("items")
                metadata = post.get("media_metadata")
                if not isinstance(entries, list) or not isinstance(metadata, dict):
                    fail("Invalid native media metadata")
                if len(entries) > max_items:
                    fail("File is larger than max-filesize")
                sources = []
                for entry in entries:
                    item = metadata.get(entry.get("media_id"), {})
                    source = item.get("s", {})
                    if item.get("status") != "valid":
                        fail("Incomplete native gallery")
                    sources.append(original(source.get("u") or source.get("gif") or source.get("mp4"), True))
            elif info and info.get("_type") == "url_transparent" and urlsplit(info.get("url", "")).hostname == "i.redd.it":
                sources = [original(info["url"])]
            elif post.get("is_video") or urlsplit(post.get("url", "")).hostname == "v.redd.it":
                fail("Video unavailable")
            else:
                fail("No media")
            if not sources:
                fail("No media")
            total = 0
            for index, (url, extension) in enumerate(sources):
                number = index + 1 if gallery is not None else 0
                with downloader.urlopen(Request(url, headers={"Accept": "*/*"})) as response:
                    length = response.headers.get("Content-Length")
                    if length and int(length) > options["max_filesize"] - total:
                        fail("File is larger than max-filesize")
                    with os.fdopen(os.open(f"item-{number:03}.{extension}", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as output:
                        while True:
                            chunk = response.read(min(65536, options["max_filesize"] - total + 1))
                            if not chunk:
                                break
                            total += len(chunk)
                            if total > options["max_filesize"]:
                                fail("File is larger than max-filesize")
                            output.write(chunk)

try:
    main()
except DownloadError:
    # YoutubeDL has already emitted a classifiable error. Suppress tracebacks,
    # which otherwise repeat post identifiers outside its normal error prefix.
    sys.exit(1)
except HTTPError as error:
    fail("HTTP Error " + str(error.status))
except RequestError:
    fail("Unable to download webpage")
except Exception:
    fail("Invalid native media output")

`

// ExtractRedditMedia downloads native Reddit media using the pinned yt-dlp session.
func (y *YTDLP) ExtractRedditMedia(ctx context.Context, request Request, proxyURL string) ([]File, error) {
	if !validAdapterRequest(request, "reddit") || !validProxyURL(proxyURL) {
		return nil, errors.New("extractor: invalid native media request")
	}
	parsed, _ := url.Parse(request.URL) // validAdapterRequest has already parsed it.
	python := filepath.Join(filepath.Dir(y.path), "python")
	result, err := y.runner.Run(ctx, Command{Path: python, Args: []string{"-I", "-c", redditMediaProgram, redditDownloaderOptions(request, proxyURL, y.media.ffmpegPath, true), ytdlpURL(parsed)}, Dir: request.WorkDir, StdoutLimit: 256 << 10, StderrLimit: 64 << 10})
	if ctx.Err() == nil && (err == nil || errors.Is(err, ErrCommandExit)) && result.Signal == "" && (bytes.Contains(result.Stdout, ytdlpTooLarge) || bytes.Contains(result.Stderr, ytdlpTooLarge)) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, toolFailure(ctx, "yt-dlp", job.DetailToolError, result, err)
	}
	names, err := discoverGallery(request.WorkDir, request.MaxItems)
	if err != nil {
		return nil, err
	}
	return y.media.Finalize(ctx, request.WorkDir, names)
}
