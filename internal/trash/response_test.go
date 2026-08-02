package trash

import (
	"strings"
	"testing"
)

func TestDecodeListRejectsUnexpectedHref(t *testing.T) {
	data := strings.Replace(
		trashResponse,
		"/dav/spaces/trash-bin/storage%24space/file-key",
		"/outside/file-key",
		1,
	)
	_, err := DecodeList([]byte(data), "/dav/spaces/trash-bin/storage$space")
	if err == nil || !strings.Contains(err.Error(), "outside expected root") {
		t.Fatalf("error: %v", err)
	}
}

func TestDecodeListRejectsInvalidNumbers(t *testing.T) {
	data := strings.Replace(trashResponse, ">12<", ">invalid<", 1)
	_, err := DecodeList([]byte(data), "/dav/spaces/trash-bin/storage$space")
	if err == nil || !strings.Contains(err.Error(), "trash item size") {
		t.Fatalf("error: %v", err)
	}
}
