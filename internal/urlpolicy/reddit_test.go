package urlpolicy

import (
	"errors"
	"testing"
)

func TestNormalizeRedditShare(t *testing.T) {
	p := newTestPolicy(t, "reddit")
	for _, trailing := range []string{"", "/"} {
		got, err := p.Normalize("https://www.reddit.com/r/example/s/Ab12Cd34Ef" + trailing + "?utm_source=example#fragment")
		if err != nil || got.Platform != "reddit" || got.URL != "https://www.reddit.com/r/example/s/Ab12Cd34Ef"+trailing {
			t.Fatalf("Normalize = %#v, %v", got, err)
		}
	}
	for _, path := range []string{
		"/r/example/s/Ab12Cd34E", "/r/example/s/Ab12Cd34Efg", "/r/example/s/Ab12Cd34E_",
		"/r/example/s/Ab12Cd34Ef/extra", "/r/example%2fs/Ab12Cd34Ef", "/user/example/s/Ab12Cd34Ef",
	} {
		if _, err := p.Normalize("https://www.reddit.com" + path); !errors.Is(err, ErrUnsupportedURL) {
			t.Fatalf("accepted share path %q: %v", path, err)
		}
	}
}
