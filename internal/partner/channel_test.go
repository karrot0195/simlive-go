package partner

import (
	"bytes"
	"strings"
	"testing"
)

func TestChannelListDocumentOrderAndEmptyImage(t *testing.T) {
	channels := []ChannelView{
		{ID: "ch-1", Name: "One", Img: "http://img/1.png"},
		{ID: "ch-2", Name: "Two", Img: ""},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, ChannelListDocument(channels, nil, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "<channel-playouts>") {
		t.Fatalf("missing root: %s", out)
	}
	// id, name, img order within each channel.
	idIdx := strings.Index(out, "<id>ch-1</id>")
	nameIdx := strings.Index(out, "<name>One</name>")
	imgIdx := strings.Index(out, "<img>http://img/1.png</img>")
	if !(idIdx < nameIdx && nameIdx < imgIdx) {
		t.Fatalf("base field order wrong: %s", out)
	}
	if !strings.Contains(out, "<img></img>") {
		t.Fatalf("expected empty img element: %s", out)
	}
}

func TestChannelListEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, ChannelListDocument(nil, nil, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "<channel-playouts></channel-playouts>") {
		t.Fatalf("expected empty root: %s", buf.String())
	}
}

func TestChannelExtraUpstreamFieldAddressable(t *testing.T) {
	raw := map[string]any{"id": "ch-1", "name": "One", "base_name": "news"}
	view, ok := NormalizeChannel(raw, &PartnerProfile{})
	if !ok {
		t.Fatal("expected channel to normalize")
	}
	fields := []XMLField{{Name: "base_name", Source: "base_name"}}
	var buf bytes.Buffer
	if err := Encode(&buf, ChannelListDocument([]ChannelView{view}, fields, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "<base_name>news</base_name>") {
		t.Fatalf("custom field missing: %s", buf.String())
	}
}
