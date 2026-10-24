// Package saavntest provides a deterministic fake of the Saavn api.php
// endpoint for tests. Fixtures mirror the field shapes the reference
// implementation reads, including the upstream's inconsistencies (numbers
// as strings, empty objects as "" or []).
package saavntest

import (
	"crypto/des"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Fake is an httptest server with call counters and failure injection.
type Fake struct {
	*httptest.Server
	Calls atomic.Int64

	mu       sync.Mutex
	byCall   map[string]int
	failNext map[string][]int // __call -> queue of status codes to return first
	delay    map[string]time.Duration
	override map[string]any // __call -> raw response
}

func New() *Fake {
	f := &Fake{byCall: map[string]int{}, failNext: map[string][]int{}, delay: map[string]time.Duration{}, override: map[string]any{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

// URL of the fake api.php.
func (f *Fake) APIURL() string { return f.Server.URL + "/api.php" }

// FailNext makes the next len(codes) calls to call return those statuses.
func (f *Fake) FailNext(call string, codes ...int) {
	f.mu.Lock()
	f.failNext[call] = append(f.failNext[call], codes...)
	f.mu.Unlock()
}

// Delay makes every call to call sleep first.
func (f *Fake) Delay(call string, d time.Duration) {
	f.mu.Lock()
	f.delay[call] = d
	f.mu.Unlock()
}

// Override replaces the response for call.
func (f *Fake) Override(call string, v any) {
	f.mu.Lock()
	f.override[call] = v
	f.mu.Unlock()
}

func (f *Fake) Count(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byCall[call]
}

func (f *Fake) handle(w http.ResponseWriter, r *http.Request) {
	f.Calls.Add(1)
	q := r.URL.Query()
	call := q.Get("__call")
	f.mu.Lock()
	f.byCall[call]++
	var code int
	if fails := f.failNext[call]; len(fails) > 0 {
		code = fails[0]
		f.failNext[call] = fails[1:]
	}
	d := f.delay[call]
	ov, hasOv := f.override[call]
	f.mu.Unlock()
	if d > 0 {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}
	if code != 0 {
		if code == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "0")
		}
		w.WriteHeader(code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if hasOv {
		_ = json.NewEncoder(w).Encode(ov)
		return
	}
	var resp any
	switch call {
	case "search.getResults":
		resp = map[string]any{"total": 3, "start": "1", "results": []any{
			Song("s1", "Kesariya", "a1", "Arijit Singh", "al1", "Brahmastra", 268),
			Song("s2", "Kesariya (Remix)", "a1", "Arijit Singh", "al1", "Brahmastra", 200),
			// The same recording listed twice under another ID.
			Song("s1dup", "Kesariya", "a1", "Arijit Singh", "al1", "Brahmastra", 269),
		}}
	case "search.getAlbumResults":
		resp = map[string]any{"total": "1", "start": 1, "results": []any{Album("al1", "Brahmastra", nil)}}
	case "search.getArtistResults":
		resp = map[string]any{"total": 1, "start": 1, "results": []any{
			map[string]any{"id": "a1", "name": "Arijit Singh", "role": "", "image": "http://c.saavncdn.com/a1-150x150.jpg", "type": "artist", "perma_url": "https://www.jiosaavn.com/artist/arijit-singh/a1"},
		}}
	case "search.getPlaylistResults":
		resp = map[string]any{"total": 1, "start": 1, "results": []any{
			map[string]any{"id": "p1", "title": "Romantic Hits", "type": "playlist", "image": "", "perma_url": "https://www.jiosaavn.com/featured/romantic/p1", "explicit_content": "0", "more_info": map[string]any{"song_count": "25", "language": "hindi"}},
		}}
	case "song.getDetails", "webapi.get":
		if call == "webapi.get" && q.Get("type") != "song" {
			resp = f.entityByLink(q.Get("type"), q.Get("token"))
			break
		}
		ids := strings.Split(q.Get("pids"), ",")
		if call == "webapi.get" {
			ids = []string{q.Get("token")}
		}
		songs := []any{}
		for _, id := range ids {
			// Like the upstream, an ID that is not a Saavn song ID (for
			// example a Swar internal ID such as "saavn:s1") finds nothing.
			if id == "" || id == "missing" || strings.Contains(id, ":") {
				continue
			}
			songs = append(songs, Song(id, "Song "+id, "a2", "Pritam", "al2", "Album "+id, 180))
		}
		resp = map[string]any{"songs": songs}
	case "webradio.createEntityStation":
		resp = map[string]any{"stationid": "station-" + q.Get("entity_id")}
	case "webradio.getSong":
		resp = map[string]any{
			"stationid": q.Get("stationid"),
			"0":         map[string]any{"song": Song("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262)},
			"1":         map[string]any{"song": Song("r2", "Channa Mereya", "a1", "Arijit Singh", "al4", "ADHM", 289)},
			"2":         map[string]any{"song": Song("r3", "Raabta", "a3", "Pritam", "al5", "Agent Vinod", 240)},
		}
	case "content.getAlbumDetails":
		resp = Album(q.Get("albumid"), "Album "+q.Get("albumid"), []any{
			Song("t1", "Track One", "a1", "Arijit Singh", q.Get("albumid"), "Album", 200),
			Song("t2", "Track Two", "a1", "Arijit Singh", q.Get("albumid"), "Album", 210),
		})
	case "artist.getArtistPageDetails":
		resp = map[string]any{
			"artistId": q.Get("artistId"), "name": "Arijit Singh", "image": "https://c.saavncdn.com/a-150x150.jpg",
			"follower_count": "1000", "isVerified": true, "dominantLanguage": "hindi", "bio": `[{"text":"Singer","title":"Bio","sequence":0}]`,
			"topSongs":       []any{Song("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262), Song("x9", "Ae Dil", "a1", "Arijit Singh", "al9", "ADHM", 250)},
			"topAlbums":      []any{Album("al3", "Aashiqui 2", nil)},
			"singles":        "",
			"similarArtists": []any{map[string]any{"id": "a3", "name": "Pritam", "perma_url": "", "image_url": ""}},
		}
	case "artist.getArtistMoreSong":
		resp = map[string]any{"topSongs": map[string]any{"total": 2, "songs": []any{
			Song("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262),
			Song("x9", "Ae Dil", "a1", "Arijit Singh", "al9", "ADHM", 250),
		}}}
	case "artist.getArtistMoreAlbum":
		resp = map[string]any{"topAlbums": map[string]any{"total": "1", "albums": []any{Album("al3", "Aashiqui 2", nil)}}}
	case "playlist.getDetails":
		resp = map[string]any{"id": q.Get("listid"), "title": "Playlist", "type": "playlist", "list_count": "2", "image": "",
			"more_info": map[string]any{"artists": []any{}},
			"list":      []any{Song("t1", "Track One", "a1", "Arijit Singh", "al1", "A", 200), Song("t2", "Track Two", "a2", "Pritam", "al2", "B", 210)}}
	case "lyrics.getLyrics":
		resp = map[string]any{"lyrics": "line one<br>line two", "lyrics_copyright": "(c) Label", "snippet": "line one"}
	case "content.getTrending":
		resp = []any{map[string]any{"id": "s1", "title": "Kesariya", "type": "song", "image": "http://c.saavncdn.com/s1-150x150.jpg"}}
	case "content.getBrowseModules":
		resp = map[string]any{"new_albums": []any{map[string]any{"id": "al1", "title": "Brahmastra", "type": "album"}}, "junk": "x"}
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *Fake) entityByLink(typ, token string) any {
	switch typ {
	case "album":
		return Album(token, "Linked Album", []any{Song("t1", "Track One", "a1", "Arijit Singh", token, "Linked Album", 200)})
	case "playlist":
		return map[string]any{"id": token, "title": "Linked Playlist", "list": []any{Song("t1", "Track One", "a1", "Arijit Singh", "al1", "A", 200)}}
	case "artist":
		return map[string]any{"artistId": token, "name": "Linked Artist"}
	}
	return map[string]any{}
}

// MediaURL is the plain URL the fake's encrypted media references decode to.
func MediaURL(id string) string { return "https://aac.saavncdn.com/" + id + "_96.mp4" }

// Encrypt produces an encrypted_media_url the way the upstream does
// (DES-ECB, PKCS#5 padding, base64).
func Encrypt(plain string) string {
	block, _ := des.NewCipher([]byte("38346591"))
	b := []byte(plain)
	pad := 8 - len(b)%8
	for i := 0; i < pad; i++ {
		b = append(b, byte(pad))
	}
	out := make([]byte, len(b))
	for i := 0; i < len(b); i += 8 {
		block.Encrypt(out[i:i+8], b[i:i+8])
	}
	return base64.StdEncoding.EncodeToString(out)
}

// Song builds a song fixture.
func Song(id, title, artistID, artist, albumID, album string, duration int) map[string]any {
	return map[string]any{
		"id": id, "title": title, "subtitle": artist + " - " + album, "type": "song",
		"perma_url": "https://www.jiosaavn.com/song/x/" + id, "image": "http://c.saavncdn.com/" + id + "-150x150.jpg",
		"language": "hindi", "year": "2022", "play_count": "12345", "explicit_content": "0",
		"more_info": map[string]any{
			"album_id": albumID, "album": album, "label": "Sony", "duration": fmt.Sprint(duration),
			"encrypted_media_url": Encrypt(MediaURL(id)), "has_lyrics": "true", "lyrics_id": id, "320kbps": "true",
			"rights":       map[string]any{"code": "0", "cacheable": "true"},
			"artistMap":    map[string]any{"primary_artists": []any{map[string]any{"id": artistID, "name": artist, "role": "singer", "type": "artist", "image": "", "perma_url": ""}}, "featured_artists": []any{}, "artists": []any{}},
			"release_date": "2022-07-17", "copyright_text": "(c) Sony",
		},
	}
}

// Album builds an album fixture.
func Album(id, title string, songs []any) map[string]any {
	m := map[string]any{
		"id": id, "title": title, "header_desc": "An album", "type": "album", "year": "2022", "language": "hindi",
		"perma_url": "https://www.jiosaavn.com/album/x/" + id, "image": "http://c.saavncdn.com/" + id + "-150x150.jpg",
		"explicit_content": "0", "more_info": map[string]any{"song_count": len(songs), "artistMap": map[string]any{"primary_artists": []any{map[string]any{"id": "a1", "name": "Arijit Singh"}}}},
	}
	if songs != nil {
		m["list"] = songs
	} else {
		m["list"] = ""
	}
	return m
}
