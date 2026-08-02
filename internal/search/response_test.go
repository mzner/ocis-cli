package search

import "testing"

func TestDecodeDirectoryUsesCollectionSize(t *testing.T) {
	data := []byte(`<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
		<d:response><d:href>/dav/spaces/storage$space/Reports</d:href>
		<d:propstat><d:prop>
			<oc:name>Reports</oc:name><oc:spaceid>storage$space</oc:spaceid>
			<oc:size>9</oc:size><d:resourcetype><d:collection/></d:resourcetype>
		</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
	</d:multistatus>`)
	items, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "directory" ||
		items[0].Path != "/Reports" || items[0].Size != 9 {
		t.Fatalf("items: %#v", items)
	}
}

func TestDecodeSkipsFailedPropstat(t *testing.T) {
	data := []byte(`<d:multistatus xmlns:d="DAV:"><d:response>
		<d:href>/dav/spaces/a$b/missing</d:href>
		<d:propstat><d:prop/><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat>
	</d:response></d:multistatus>`)
	items, err := Decode(data)
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestDecodeFallsBackToSpaceIDFromHref(t *testing.T) {
	data := []byte(`<d:multistatus xmlns:d="DAV:"><d:response>
		<d:href>/remote.php/dav/spaces/storage$space/file.txt</d:href>
		<d:propstat><d:prop/><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
	</d:response></d:multistatus>`)
	items, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SpaceID != "storage$space" {
		t.Fatalf("items: %#v", items)
	}
}

func TestDecodeRejectsMalformedProperties(t *testing.T) {
	for name, data := range map[string]string{
		"href": `<d:multistatus xmlns:d="DAV:"><d:response>
			<d:href>/unexpected</d:href><d:propstat><d:prop/>
			<d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`,
		"size": `<d:multistatus xmlns:d="DAV:"><d:response>
			<d:href>/dav/spaces/a$b/file</d:href><d:propstat><d:prop>
			<d:getcontentlength>many</d:getcontentlength></d:prop>
			<d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`,
		"score": `<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:response>
			<d:href>/dav/spaces/a$b/file</d:href><d:propstat><d:prop>
			<oc:score>high</oc:score></d:prop>
			<d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(data)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseContentRange(t *testing.T) {
	for value, want := range map[string]int{
		"": 0, "rows 0-9/25": 25, "ROWS 10-19/100": 100,
	} {
		got, err := ParseContentRange(value)
		if err != nil || got != want {
			t.Fatalf("%q: got %d, %v", value, got, err)
		}
	}
	if _, err := ParseContentRange("items 0-1/2"); err == nil {
		t.Fatal("accepted malformed range")
	}
}
