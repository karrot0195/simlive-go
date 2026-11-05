package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"simlive/internal/partner"
)

func TestNew_DefaultRclone(t *testing.T) {
	p := &partner.PartnerProfile{}
	d, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.(*RcloneDownload); !ok {
		t.Fatalf("expected RcloneDownload, got %T", d)
	}
}

func TestNew_HTTP(t *testing.T) {
	p := &partner.PartnerProfile{
		DataProvider: "http",
		Data: &partner.PartnerData{
			Endpoint: "http://example.com/data",
			Headers:  map[string]string{"X-Test": "1"},
		},
	}
	d, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.(*ClientDownload); !ok {
		t.Fatalf("expected ClientDownload, got %T", d)
	}
}

func TestNew_HTTPMissingEndpoint(t *testing.T) {
	p := &partner.PartnerProfile{
		DataProvider: "http",
	}
	if _, err := New(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestNew_UnknownProvider(t *testing.T) {
	p := &partner.PartnerProfile{
		DataProvider: "ftp",
	}
	if _, err := New(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestClientDownload_Fetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "1" {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	d := NewClient(srv.URL, map[string]string{"X-Test": "1"}, 0)
	b, err := d.Fetch(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("got %s", string(b))
	}
}

func TestClientDownload_FetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	d := NewClient(srv.URL, nil, 0)
	if _, err := d.Fetch(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}
