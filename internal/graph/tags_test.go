package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestTagMutationsUseLibreGraphExtension(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != tagsResource {
			t.Fatalf("path: %s", request.URL.Path)
		}
		var body tagMutationRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ResourceID != "storage$space!item" ||
			len(body.Tags) != 2 || body.Tags[0] != "approved" {
			t.Fatalf("body: %#v", body)
		}
		methods = append(methods, request.Method)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(
		httpapi.Config{Server: server.URL}, server.Client(),
	)
	tags := []string{"approved", "quarterly"}
	if err := client.AddTags(
		context.Background(), "storage$space!item", tags,
	); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveTags(
		context.Background(), "storage$space!item", tags,
	); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 ||
		methods[0] != http.MethodPut ||
		methods[1] != http.MethodDelete {
		t.Fatalf("methods: %#v", methods)
	}
}
