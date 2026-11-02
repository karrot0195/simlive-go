package partner

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncodeDeterministicOrder(t *testing.T) {
	root := NewNode("channel-playouts")
	ch := root.AddChild(NewNode("channel"))
	ch.AddTextChild("id", "ch-1")
	ch.AddTextChild("name", "Channel One")
	ch.AddTextChild("img", "https://cdn/ch-1.png")

	var buf bytes.Buffer
	if err := Encode(&buf, root); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	idxID := strings.Index(out, "<id>")
	idxName := strings.Index(out, "<name>")
	idxImg := strings.Index(out, "<img>")
	if !(idxID < idxName && idxName < idxImg) {
		t.Fatalf("element order wrong: %s", out)
	}
	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("missing xml declaration: %s", out)
	}
}

func TestEncodeEscaping(t *testing.T) {
	root := NewNode("x")
	root.AddTextChild("name", `A & B <c> "d"`)
	var buf bytes.Buffer
	if err := Encode(&buf, root); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<c>") || strings.Contains(out, `"d"`) {
		t.Fatalf("content not escaped: %s", out)
	}
	if !strings.Contains(out, "&amp;") || !strings.Contains(out, "&lt;") {
		t.Fatalf("expected escaped entities: %s", out)
	}
}

func TestEncodeEmptyElement(t *testing.T) {
	root := NewNode("channel")
	root.AddTextChild("img", "")
	var buf bytes.Buffer
	if err := Encode(&buf, root); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "<img></img>") {
		t.Fatalf("expected empty img element, got: %s", buf.String())
	}
}

func TestErrorDocument(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, ErrorDocument("channel_not_found", "Channel not found")); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<error>", "<code>channel_not_found</code>", "<message>Channel not found</message>", "</error>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
}

func TestChannelListDocumentBaseFields(t *testing.T) {
	channels := []ChannelView{
		{ID: "ch-1", Name: "One", Img: "http://img/1.png"},
		{ID: "ch-2", Name: "Two", Img: ""},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, ChannelListDocument(channels, nil, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "<channel>") != 2 {
		t.Fatalf("expected 2 channels: %s", out)
	}
	if !strings.Contains(out, "<img></img>") {
		t.Fatalf("expected empty img for channel without image: %s", out)
	}
}

func TestEPGDocumentBaseFields(t *testing.T) {
	items := []EPGItemView{
		{Start: "2026-11-02T00:00:00+07:00", Stop: "2026-11-02T01:00:00+07:00", StreamID: "s1", Ads: false},
		{Start: "2026-11-02T01:00:00+07:00", Stop: "2026-11-02T01:00:30+07:00", StreamID: "ads-1", Ads: true},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, EPGDocument("ch-1", items, nil, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "<channelId>ch-1</channelId>") {
		t.Fatalf("missing channelId: %s", out)
	}
	if !strings.Contains(out, "<ads>true</ads>") || !strings.Contains(out, "<ads>false</ads>") {
		t.Fatalf("expected lowercase ads values: %s", out)
	}
}
