package ytmusic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A trimmed copy of a real "Songs" search response from music.youtube.com.
const sample = `{"contents":{"tabbedSearchResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"musicShelfRenderer":{"title":{"runs":[{"text":"Songs"}]},"contents":[
{"musicResponsiveListItemRenderer":{
 "thumbnail":{"musicThumbnailRenderer":{"thumbnail":{"thumbnails":[{"url":"https://lh3.googleusercontent.com/abc=w60-h60-l90-rj","width":60,"height":60},{"url":"https://lh3.googleusercontent.com/abc=w120-h120-l90-rj","width":120,"height":120}]}}},
 "flexColumns":[
  {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"Kesariya","navigationEndpoint":{"watchEndpoint":{"videoId":"BddP6PYo2gs"}}}]}}},
  {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[
   {"text":"Pritam","navigationEndpoint":{"browseEndpoint":{"browseId":"UCpritam","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ARTIST"}}}}},
   {"text":" & "},
   {"text":"Arijit Singh","navigationEndpoint":{"browseEndpoint":{"browseId":"UCarijit","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ARTIST"}}}}},
   {"text":" • "},
   {"text":"Brahmastra","navigationEndpoint":{"browseEndpoint":{"browseId":"MPREb_x","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ALBUM"}}}}},
   {"text":" • "},
   {"text":"4:28"}]}}}],
 "playlistItemData":{"videoId":"BddP6PYo2gs"}}},
{"musicResponsiveListItemRenderer":{
 "flexColumns":[
  {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"Kesariya (Dance Mix)","navigationEndpoint":{"watchEndpoint":{"videoId":"xyzXYZ12345"}}}]}}},
  {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"Some Artist"},{"text":" • "},{"text":"1:02:03"}]}}}]}},
{"musicResponsiveListItemRenderer":{"flexColumns":[{"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"No video id"}]}}}]}}
]}}]}}}}]}}}`

func TestParseSongs(t *testing.T) {
	got, err := Parse([]byte(sample), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 songs, got %d: %+v", len(got), got)
	}
	k := got[0]
	if k.ID != "yt:BddP6PYo2gs" || k.Title != "Kesariya" || k.DurationSec != 268 {
		t.Fatalf("first song: %+v", k)
	}
	if len(k.Artists) != 2 || k.Artists[1].Name != "Arijit Singh" || k.Album == nil || k.Album.Name != "Brahmastra" {
		t.Fatalf("artists/album: %+v %+v", k.Artists, k.Album)
	}
	if len(k.Images) != 1 || k.Images[0].URL != "https://lh3.googleusercontent.com/abc=w544-h544-l90-rj" {
		t.Fatalf("image: %+v", k.Images)
	}
	if got[1].Artists[0].Name != "Some Artist" || got[1].DurationSec != 3723 {
		t.Fatalf("second song: %+v", got[1])
	}
	if one, _ := Parse([]byte(sample), 1); len(one) != 1 {
		t.Fatalf("limit not applied: %d", len(one))
	}
}

func TestSearchSendsSongsFilter(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	c := New(0)
	c.endpoint = srv.URL
	got, err := c.SearchSongs(context.Background(), "kesariya", 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("search: %v %d", err, len(got))
	}
	for _, want := range []string{`"query":"kesariya"`, songsOnly, "WEB_REMIX"} {
		if !contains(body, want) {
			t.Errorf("request body missing %s: %s", want, body)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
