package browser

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestWriteScreenshot(t *testing.T) {
	c := NewController(ControllerOptions{Config: testBrowserConfig(), BaseDir: t.TempDir()})
	out, err := c.writeScreenshot(base64.StdEncoding.EncodeToString([]byte("png-bytes")), "png")
	if err != nil {
		t.Fatal(err)
	}
	if out.Bytes != len("png-bytes") {
		t.Fatalf("bytes = %d", out.Bytes)
	}
	data, err := os.ReadFile(out.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("data = %q", data)
	}
}
