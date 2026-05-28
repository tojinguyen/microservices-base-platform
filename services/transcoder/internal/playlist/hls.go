package playlist

import (
	"fmt"
	"strings"

	"github.com/tojinguyen/transcoder/internal/ffmpeg"
)

// GenerateMaster builds an HLS master playlist with relative URIs for each
// rendition's child playlist. Audio-only renditions become EXT-X-MEDIA entries.
func GenerateMaster(renditions []ffmpeg.RenditionPath) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n\n")

	hasAudio := false
	for _, r := range renditions {
		if r.AudioOnly {
			fmt.Fprintf(&b, `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Default",DEFAULT=YES,URI="%s/%s.m3u8"`+"\n",
				r.Name, r.Name)
			hasAudio = true
		}
	}
	if hasAudio {
		b.WriteString("\n")
	}

	for _, r := range renditions {
		if r.AudioOnly {
			continue
		}
		bandwidth := r.BitrateKbps * 1000
		codecs := videoCodecsForBitrate(r.BitrateKbps)
		if hasAudio {
			fmt.Fprintf(&b, `#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS="%s",AUDIO="audio"`+"\n",
				bandwidth, r.Width, r.Height, codecs)
		} else {
			fmt.Fprintf(&b, `#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS="%s"`+"\n",
				bandwidth, r.Width, r.Height, codecs)
		}
		fmt.Fprintf(&b, "%s/%s.m3u8\n", r.Name, r.Name)
	}

	return b.String()
}

func videoCodecsForBitrate(kbps int) string {
	switch {
	case kbps >= 4000:
		return "avc1.640028,mp4a.40.2"
	case kbps >= 2000:
		return "avc1.4d401f,mp4a.40.2"
	default:
		return "avc1.42e01e,mp4a.40.2"
	}
}
