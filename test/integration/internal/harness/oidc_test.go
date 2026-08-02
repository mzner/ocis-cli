package harness

import (
	"net/url"
	"testing"
)

func TestHelloFromURL(t *testing.T) {
	value, err := url.Parse(
		"https://idp.test/signin?state=s&scope=openid+profile" +
			"&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1",
	)
	if err != nil {
		t.Fatal(err)
	}
	hello := helloFromURL(value)
	if hello.State != "s" || hello.Flow != "oidc" ||
		hello.ClientID != "client" || hello.Scope != "openid profile" {
		t.Fatalf("hello = %#v", hello)
	}
}
