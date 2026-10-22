package saavn

import (
	"context"
	"crypto/des"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"swar/internal/catalog"
)

// mediaKey is the publicly known key the Saavn web player uses for its
// media references. Resolution is disabled unless the operator enables
// SAAVN_MEDIA_URLS_ENABLED (see docs/DECISIONS.md).
const mediaKey = "38346591"

// assumedMediaTTL is how long a resolved Saavn stream reference is treated
// as valid. Saavn does not publish an expiry, so this is conservative and
// clients re-resolve after it.
const assumedMediaTTL = 3 * time.Hour

var qualities = []struct {
	suffix, label string
	kbps          int
}{
	{"_12", "12kbps", 12}, {"_48", "48kbps", 48}, {"_96", "96kbps", 96},
	{"_160", "160kbps", 160}, {"_320", "320kbps", 320},
}

// MediaSources resolves stream references for one song.
func (c *Client) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	if !c.mediaURLs {
		return nil, time.Time{}, catalog.Unavailable("streaming for this source is not enabled on this server")
	}
	var resp rawSongsResponse
	if err := c.get(ctx, callSongDetails, url.Values{"pids": {id}}, ctxWeb, &resp); err != nil {
		return nil, time.Time{}, err
	}
	if len(resp.Songs) == 0 {
		return nil, time.Time{}, catalog.NotFound("track not found")
	}
	enc := string(resp.Songs[0].MoreInfo.V.EncryptedMediaURL)
	if enc == "" {
		return nil, time.Time{}, catalog.Unavailable("this track has no playable source")
	}
	sources, err := decodeMediaURL(enc)
	if err != nil {
		return nil, time.Time{}, catalog.NewError(catalog.KindUpstream, "could not read the track's media reference", err)
	}
	return sources, time.Now().Add(assumedMediaTTL), nil
}

func decodeMediaURL(enc string) ([]catalog.MediaSource, error) {
	data, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	block, err := des.NewCipher([]byte(mediaKey))
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()
	if len(data) == 0 || len(data)%bs != 0 {
		return nil, errors.New("ciphertext is not a whole number of blocks")
	}
	plain := make([]byte, len(data))
	for i := 0; i < len(data); i += bs {
		block.Decrypt(plain[i:i+bs], data[i:i+bs]) // ECB mode
	}
	if pad := int(plain[len(plain)-1]); pad >= 1 && pad <= bs {
		plain = plain[:len(plain)-pad]
	}
	link := string(plain)
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return nil, errors.New("decoded media reference is not a URL")
	}
	if u.Scheme == "http" {
		link = "https://" + strings.TrimPrefix(link, "http://")
	}
	out := make([]catalog.MediaSource, 0, len(qualities))
	for _, q := range qualities {
		out = append(out, catalog.MediaSource{
			Kind: "audio", Quality: q.label, Bitrate: q.kbps, MimeType: "audio/mp4",
			URL: strings.Replace(link, "_96", q.suffix, 1),
		})
	}
	return out, nil
}
