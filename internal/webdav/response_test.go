package webdav

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDecodeListRejectsMalformedEscapes(t *testing.T) {
	data := []byte(`<multistatus xmlns="DAV:"><response><href>/root/</href><propstat><status>HTTP/1.1 200 OK</status><prop><resourcetype><collection/></resourcetype></prop></propstat></response><response><href>/root/%zz</href><propstat><status>HTTP/1.1 200 OK</status><prop/></propstat></response></multistatus>`)
	if _, err := DecodeList(data, "/root"); err == nil {
		t.Fatal("DecodeList accepted malformed escaped name")
	}
}

func TestDecodeStatRejectsMalformedXML(t *testing.T) {
	if _, err := DecodeStat([]byte(`<multistatus>`), "/"); err == nil {
		t.Fatal("DecodeStat accepted malformed XML")
	}
}

func TestDecodeStatIncludesStableResourceID(t *testing.T) {
	data := []byte(`<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">` +
		`<d:response><d:href>/remote.php/dav/files/alice/report.txt</d:href>` +
		`<d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>` +
		`<d:displayname>report.txt</d:displayname>` +
		`<d:getcontentlength>42</d:getcontentlength>` +
		`<oc:fileid>storage$space!file</oc:fileid>` +
		`</d:prop></d:propstat></d:response></d:multistatus>`)

	item, err := DecodeStat(data, "/report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := item.ResourceID, "storage$space!file"; got != want {
		t.Fatalf("resource ID = %q, want %q", got, want)
	}
}

func TestDecodeStatIncludesFileMetadata(t *testing.T) {
	data := []byte(`<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">` +
		`<d:response><d:href>/dav/report.txt</d:href>` +
		`<d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>` +
		`<oc:tags>approved, quarterly</oc:tags>` +
		`<oc:favorite>1</oc:favorite>` +
		`<oc:checksums><oc:checksum>` +
		`SHA1:abc MD5:def ADLER32:123` +
		`</oc:checksum></oc:checksums>` +
		`</d:prop></d:propstat></d:response></d:multistatus>`)

	item, err := DecodeStat(data, "/report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if item.Favorite == nil || !*item.Favorite ||
		len(item.Tags) != 2 || item.Tags[1] != "quarterly" ||
		len(item.Checksums) != 3 ||
		item.Checksums[0].Algorithm != "SHA1" ||
		item.Checksums[0].Value != "abc" {
		t.Fatalf("metadata: %#v", item)
	}
}

func TestEscapeRemote(t *testing.T) {
	if got, want := escapeRemote("/Project docs/hello #1.txt"), "/Project%20docs/hello%20%231.txt"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResponseErrorIsBounded(t *testing.T) {
	response := &http.Response{Status: "401 Unauthorized", StatusCode: 401, Body: io.NopCloser(strings.NewReader("denied"))}
	if got := responseError(response).Error(); got != "401 Unauthorized: denied" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestResponseErrorExtractsDAVMessage(t *testing.T) {
	response := &http.Response{
		Status: "404 Not Found", StatusCode: 404,
		Body: io.NopCloser(strings.NewReader(
			`<d:error xmlns:d="DAV:" xmlns:s="http://sabredav.org/ns">` +
				`<s:message>Resource not found</s:message></d:error>`,
		)),
	}
	if got := responseError(response).Error(); got !=
		"404 Not Found: Resource not found" {
		t.Fatalf("unexpected error: %s", got)
	}
}
