package core

import (
	"net/url"
	"regexp"
	"strings"
)

// ImageAttachment is optional rich media metadata for channel responses.
type ImageAttachment struct {
	URL     string `json:"url"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// OutboundResponse is the channel-facing representation of an engine reply.
// Text is always the fallback body; Image is optional rich media metadata.
type OutboundResponse struct {
	Text  string           `json:"text"`
	Image *ImageAttachment `json:"image,omitempty"`
}

var markdownImageRE = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)

// ParseOutboundResponse extracts a single image attachment from assistant text
// while preserving the original text as fallback.
func ParseOutboundResponse(text string) OutboundResponse {
	out := OutboundResponse{Text: text}

	match := markdownImageRE.FindStringSubmatchIndex(text)
	if match != nil {
		alt := text[match[2]:match[3]]
		imageURL := text[match[4]:match[5]]
		if isSupportedImageAttachmentURL(imageURL, true) {
			caption := strings.TrimSpace(text[:match[0]] + text[match[1]:])
			if caption == "" {
				caption = alt
			}
			out.Image = &ImageAttachment{URL: imageURL, Alt: alt, Caption: caption}
			return out
		}
	}

	trimmed := strings.TrimSpace(text)
	if isSupportedImageAttachmentURL(trimmed, false) {
		out.Image = &ImageAttachment{URL: trimmed}
	}
	return out
}

func isSupportedImageAttachmentURL(raw string, fromMarkdownImage bool) bool {
	if strings.HasPrefix(raw, "data:image/") && strings.Contains(raw, ";base64,") {
		return true
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	if fromMarkdownImage {
		return true
	}

	path := strings.ToLower(parsed.Path)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
