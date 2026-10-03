"""Test-process HTTP injection for real gallery-dl and yt-dlp downloads.

The Go integration test provides fixture configuration through runpy. This
module never runs in production and never changes application proxy policy.
"""

import urllib.parse
import sys

import gallery_dl
import requests
import yt_dlp
from yt_dlp.networking import RequestDirector


def local_url(raw):
    if raw.startswith(fixture["base"] + "/"):
        return raw
    parsed = urllib.parse.urlsplit(raw)
    assert parsed.hostname in {"www.reddit.com", "i.redd.it", "v.redd.it", "preview.redd.it"}
    return fixture["base"] + parsed.path + ("?" + parsed.query if parsed.query else "")


original_send = requests.Session.send


def send(session, request, **kwargs):
    if not request.url.startswith(fixture["base"] + "/"):
        assert kwargs.get("proxies", {}).get("http") == fixture["proxy"]
    request.url = local_url(request.url)
    kwargs["proxies"] = {}
    return original_send(session, request, **kwargs)


requests.Session.send = send
if fixture.get("native_only"):
    from yt_dlp.extractor.reddit import RedditIE
    # Session setup is outside this local parser/downloader fixture.
    RedditIE._real_initialize = lambda self: None
    RedditIE._get_cookies = lambda self, url: {"loid": True}

original_init = yt_dlp.YoutubeDL.__init__


def init(downloader, params=None, *args, **kwargs):
    assert params["proxy"] == fixture["proxy"]
    assert params["ffmpeg_location"] == fixture["ffmpeg"]
    assert params["ignoreerrors"] is False
    assert params["max_filesize"] == fixture["max_bytes"]
    assert params["remote_components"] == []
    if fixture.get("native_only"):
        assert params["extract_flat"] is True
        assert params["allowed_extractors"] == ["reddit"]
        assert params["skip_unavailable_fragments"] is False
    return original_init(downloader, params, *args, **kwargs)


yt_dlp.YoutubeDL.__init__ = init
original_director_send = RequestDirector.send


def director_send(director, request):
    request.url = local_url(request.url)
    request.proxies = {"all": None, "http": None, "https": None}
    return original_director_send(director, request)


RequestDirector.send = director_send
if fixture.get("native_only"):
    program = sys.argv[3]
    sys.argv = ["native-media", sys.argv[4], sys.argv[5]]
    exec(program)
else:
    sys.exit(gallery_dl.main())
