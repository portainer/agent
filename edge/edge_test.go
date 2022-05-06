package edge

import (
	"net/http"
	"testing"
)

func TestBuildTransport(t *testing.T) {
	_, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("type assertion for http.DefaultTransport failed")
	}
}
