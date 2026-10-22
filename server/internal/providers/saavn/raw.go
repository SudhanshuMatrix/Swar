package saavn

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// The Saavn API is inconsistent: numbers arrive as strings or numbers,
// empty objects arrive as "" or [], and fields come and go. These helper
// types make decoding tolerant so that one odd field never fails a whole
// response.

// fstr decodes any JSON scalar into a string; objects and arrays become "".
type fstr string

func (s *fstr) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil
	}
	switch b[0] {
	case '"':
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return nil
		}
		*s = fstr(strings.TrimSpace(v))
	case 'n':
		*s = ""
	case '{', '[':
		*s = ""
	default: // number or bool
		*s = fstr(string(b))
	}
	return nil
}

func (s fstr) String() string { return string(s) }

func (s fstr) Int() int {
	n, _ := strconv.Atoi(strings.TrimSpace(string(s)))
	return n
}

func (s fstr) Int64() int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(string(s)), 10, 64)
	return n
}

func (s fstr) Bool() bool {
	switch strings.ToLower(string(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// obj decodes a JSON object into V and silently ignores anything else.
type obj[T any] struct{ V T }

func (o *obj[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil
	}
	o.V = v
	return nil
}

// list decodes a JSON array, skipping elements that fail to decode, and
// treats non-arrays as empty.
type list[T any] []T

func (l *list[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '[' {
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(b, &raws); err != nil {
		return nil
	}
	out := make([]T, 0, len(raws))
	for _, r := range raws {
		var v T
		if err := json.Unmarshal(r, &v); err == nil {
			out = append(out, v)
		}
	}
	*l = out
	return nil
}

type rawArtistMapEntry struct {
	ID       fstr `json:"id"`
	Name     fstr `json:"name"`
	Role     fstr `json:"role"`
	Image    fstr `json:"image"`
	Type     fstr `json:"type"`
	PermaURL fstr `json:"perma_url"`
}

type rawArtistMap struct {
	Primary  list[rawArtistMapEntry] `json:"primary_artists"`
	Featured list[rawArtistMapEntry] `json:"featured_artists"`
	All      list[rawArtistMapEntry] `json:"artists"`
}

type rawRights struct {
	Code      fstr `json:"code"`
	Cacheable fstr `json:"cacheable"`
	Reason    fstr `json:"reason"`
}

type rawSongMore struct {
	Music             fstr                    `json:"music"`
	AlbumID           fstr                    `json:"album_id"`
	Album             fstr                    `json:"album"`
	Label             fstr                    `json:"label"`
	EncryptedMediaURL fstr                    `json:"encrypted_media_url"`
	KBPS320           fstr                    `json:"320kbps"`
	AlbumURL          fstr                    `json:"album_url"`
	Duration          fstr                    `json:"duration"`
	Rights            obj[rawRights]          `json:"rights"`
	HasLyrics         fstr                    `json:"has_lyrics"`
	LyricsSnippet     fstr                    `json:"lyrics_snippet"`
	CopyrightText     fstr                    `json:"copyright_text"`
	ArtistMap         obj[rawArtistMap]       `json:"artistMap"`
	ReleaseDate       fstr                    `json:"release_date"`
	LyricsID          fstr                    `json:"lyrics_id"`
	PrimaryArtists    fstr                    `json:"primary_artists"`
	Singers           fstr                    `json:"singers"`
	Language          fstr                    `json:"language"`
	SongCount         fstr                    `json:"song_count"`
	Year              fstr                    `json:"year"`
	SongPids          fstr                    `json:"song_pids"`
	Artists           list[rawArtistMapEntry] `json:"artists"`
}

type rawSong struct {
	ID              fstr             `json:"id"`
	Title           fstr             `json:"title"`
	Subtitle        fstr             `json:"subtitle"`
	HeaderDesc      fstr             `json:"header_desc"`
	Type            fstr             `json:"type"`
	PermaURL        fstr             `json:"perma_url"`
	Image           fstr             `json:"image"`
	Language        fstr             `json:"language"`
	Year            fstr             `json:"year"`
	PlayCount       fstr             `json:"play_count"`
	ExplicitContent fstr             `json:"explicit_content"`
	ListCount       fstr             `json:"list_count"`
	Description     fstr             `json:"description"`
	MoreInfo        obj[rawSongMore] `json:"more_info"`
	List            list[rawSong]    `json:"list"`
}

// rawAlbum and rawPlaylist share rawSong's shape (title, image, more_info,
// list of songs), which is how the upstream models them too.
type rawAlbum = rawSong
type rawPlaylist = rawSong

type rawPaged[T any] struct {
	Total   fstr    `json:"total"`
	Start   fstr    `json:"start"`
	Results list[T] `json:"results"`
}

type rawSongsResponse struct {
	Songs list[rawSong] `json:"songs"`
}

type rawSimilarArtist struct {
	ID       fstr `json:"id"`
	Name     fstr `json:"name"`
	PermaURL fstr `json:"perma_url"`
	ImageURL fstr `json:"image_url"`
}

type rawArtist struct {
	ArtistID fstr `json:"artistId"`
	ID       fstr `json:"id"`
	Name     fstr `json:"name"`
	URLs     obj[struct {
		Overview fstr `json:"overview"`
	}] `json:"urls"`
	PermaURL         fstr                   `json:"perma_url"`
	Image            fstr                   `json:"image"`
	FollowerCount    fstr                   `json:"follower_count"`
	IsVerified       fstr                   `json:"isVerified"`
	DominantLanguage fstr                   `json:"dominantLanguage"`
	DominantType     fstr                   `json:"dominantType"`
	Bio              fstr                   `json:"bio"`
	IsRadioPresent   fstr                   `json:"isRadioPresent"`
	TopSongs         list[rawSong]          `json:"topSongs"`
	TopAlbums        list[rawAlbum]         `json:"topAlbums"`
	Singles          list[rawSong]          `json:"singles"`
	SimilarArtists   list[rawSimilarArtist] `json:"similarArtists"`
}

type rawArtistSongs struct {
	TopSongs obj[struct {
		Total fstr          `json:"total"`
		Songs list[rawSong] `json:"songs"`
	}] `json:"topSongs"`
}

type rawArtistAlbums struct {
	TopAlbums obj[struct {
		Total  fstr           `json:"total"`
		Albums list[rawAlbum] `json:"albums"`
	}] `json:"topAlbums"`
}

type rawLyrics struct {
	Lyrics    fstr `json:"lyrics"`
	Snippet   fstr `json:"snippet"`
	Copyright fstr `json:"lyrics_copyright"`
}
