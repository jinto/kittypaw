package core

import "testing"

func TestParseOutboundResponseMarkdownImage(t *testing.T) {
	out := ParseOutboundResponse("![space cat](https://cdn.example.com/cat.png)")
	if out.Text != "![space cat](https://cdn.example.com/cat.png)" {
		t.Fatalf("fallback text = %q", out.Text)
	}
	if out.Image == nil {
		t.Fatal("expected image attachment")
	}
	if out.Image.URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("url = %q", out.Image.URL)
	}
	if out.Image.Alt != "space cat" {
		t.Fatalf("alt = %q", out.Image.Alt)
	}
	if out.Image.Caption != "space cat" {
		t.Fatalf("caption = %q", out.Image.Caption)
	}
}

func TestParseOutboundResponseMarkdownImageWithCaption(t *testing.T) {
	out := ParseOutboundResponse("완성했습니다.\n\n![space cat](data:image/png;base64,aW1n)")
	if out.Image == nil {
		t.Fatal("expected image attachment")
	}
	if out.Image.Caption != "완성했습니다." {
		t.Fatalf("caption = %q", out.Image.Caption)
	}
}

func TestParseOutboundResponseSingleImageURL(t *testing.T) {
	out := ParseOutboundResponse("https://cdn.example.com/cat.webp?sig=1")
	if out.Image == nil {
		t.Fatal("expected image attachment")
	}
	if out.Image.URL != "https://cdn.example.com/cat.webp?sig=1" {
		t.Fatalf("url = %q", out.Image.URL)
	}
}

func TestParseOutboundResponseIgnoresNonImageMarkdownLink(t *testing.T) {
	out := ParseOutboundResponse("[docs](https://example.com/readme)")
	if out.Image != nil {
		t.Fatalf("unexpected image: %#v", out.Image)
	}
}

func TestNewDoneMsgForTurnWithOutboundIncludesImage(t *testing.T) {
	msg := NewDoneMsgForTurnWithOutbound("turn-1", OutboundResponse{
		Text: "fallback",
		Image: &ImageAttachment{
			URL:     "https://cdn.example.com/cat.png",
			Alt:     "cat",
			Caption: "caption",
		},
	}, nil)
	if msg.FullText != "fallback" || msg.TurnID != "turn-1" {
		t.Fatalf("message = %#v", msg)
	}
	if msg.Image == nil || msg.Image.URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("image = %#v", msg.Image)
	}
}
