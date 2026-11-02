package partner

import (
	"bytes"
	"strings"
	"testing"
)

func TestCustomFieldsOrderedAndLiteral(t *testing.T) {
	channels := []ChannelView{
		{ID: "ch-1", Name: "One", Img: "http://img/1.png", Raw: map[string]any{"base_name": "news"}},
	}
	fields := []XMLField{
		{Name: "base_name", Source: "base_name"},
		{Name: "provider", Value: "partner-a"},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, ChannelListDocument(channels, fields, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	imgIdx := strings.Index(out, "</img>")
	baseIdx := strings.Index(out, "<base_name>news</base_name>")
	provIdx := strings.Index(out, "<provider>partner-a</provider>")
	if !(imgIdx < baseIdx && baseIdx < provIdx) {
		t.Fatalf("custom field order wrong: %s", out)
	}
}

func TestCustomFieldDefault(t *testing.T) {
	items := []EPGItemView{
		{StreamID: "s1", Raw: map[string]any{"programme": map[string]any{}}},
	}
	fields := []XMLField{{Name: "episode", Source: "programme.episode", Default: "n/a"}}
	var buf bytes.Buffer
	if err := Encode(&buf, EPGDocument("ch-1", items, fields, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "<episode>n/a</episode>") {
		t.Fatalf("expected default value: %s", buf.String())
	}
}

func TestCustomFieldEmptyEmitsEmptyElement(t *testing.T) {
	items := []EPGItemView{
		{StreamID: "s1", Raw: map[string]any{"programme": map[string]any{}}},
	}
	fields := []XMLField{{Name: "episode", Source: "programme.episode"}}
	var buf bytes.Buffer
	if err := Encode(&buf, EPGDocument("ch-1", items, fields, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "<episode></episode>") {
		t.Fatalf("expected empty element: %s", buf.String())
	}
}

func TestValidFieldsSkipInvalidWithoutBreakingBase(t *testing.T) {
	channels := []ChannelView{{ID: "ch-1", Name: "One", Img: ""}}
	fields, _ := ValidCustomFields([]XMLField{
		{Name: "id", Value: "reserved"},
		{Name: "provider", Value: "a"},
	})
	var buf bytes.Buffer
	if err := Encode(&buf, ChannelListDocument(channels, fields, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<id>reserved</id>") {
		t.Fatalf("invalid field should be skipped: %s", out)
	}
	if !strings.Contains(out, "<id>ch-1</id>") || !strings.Contains(out, "<provider>a</provider>") {
		t.Fatalf("base + valid custom field expected: %s", out)
	}
}
