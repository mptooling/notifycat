package httpx

import (
	"mime"
	"net/http"
	"strconv"
	"strings"
)

type mediaRange struct {
	mediaType string
	subtype   string
	quality   float64
}

// WriteText writes text and a trailing newline as a non-cacheable plain-text response.
func WriteText(w http.ResponseWriter, status int, text string) {
	w.Header().Set(ContentTypeHeader, TextContentType)
	w.Header().Set(CacheControlHeader, NoStore)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text + "\n"))
}

// PrefersText reports whether the Accept header ranks text/plain strictly above application/json.
func PrefersText(request *http.Request) bool {
	ranges := parseAccept(request.Header.Values(AcceptHeader))
	return quality(ranges, TextMediaType) > quality(ranges, JSONMediaType)
}

func parseAccept(headers []string) []mediaRange {
	var ranges []mediaRange
	for _, header := range headers {
		for _, element := range strings.Split(header, ",") {
			parsed, ok := parseMediaRange(element)
			if ok {
				ranges = append(ranges, parsed)
			}
		}
	}
	return ranges
}

func parseMediaRange(element string) (mediaRange, bool) {
	fullType, parameters, err := mime.ParseMediaType(strings.TrimSpace(element))
	if err != nil {
		return mediaRange{}, false
	}
	mediaType, subtype, ok := strings.Cut(fullType, "/")
	if !ok {
		return mediaRange{}, false
	}
	parsed := mediaRange{mediaType: mediaType, subtype: subtype, quality: 1}
	if rawQuality, present := parameters[QualityParameter]; present {
		parsed.quality, err = strconv.ParseFloat(rawQuality, 64)
		if err != nil || parsed.quality < 0 || parsed.quality > 1 {
			return mediaRange{}, false
		}
	}
	return parsed, true
}

// quality returns the weight of the most specific range that matches fullType, or 0.
func quality(ranges []mediaRange, fullType string) float64 {
	mediaType, subtype, _ := strings.Cut(fullType, "/")
	bestSpecificity, bestQuality := -1, 0.0
	for _, candidate := range ranges {
		specificity, matches := candidate.match(mediaType, subtype)
		if matches && specificity > bestSpecificity {
			bestSpecificity, bestQuality = specificity, candidate.quality
		}
	}
	return bestQuality
}

func (candidate mediaRange) match(mediaType, subtype string) (int, bool) {
	switch {
	case candidate.mediaType == mediaType && candidate.subtype == subtype:
		return exactTypeSpecificity, true
	case candidate.mediaType == mediaType && candidate.subtype == MediaWildcard:
		return subtypeWildcardSpecificity, true
	case candidate.mediaType == MediaWildcard && candidate.subtype == MediaWildcard:
		return anyTypeSpecificity, true
	default:
		return anyTypeSpecificity, false
	}
}
