package partner

import (
	"encoding/xml"
	"io"
)

// Node is a dynamic, ordered XML element used to build responses with a
// deterministic element order (base fields first, then custom fields).
type Node struct {
	Name     string
	Text     string
	Children []*Node
}

// NewNode creates an element node with the given name.
func NewNode(name string) *Node {
	return &Node{Name: name}
}

// AddChild appends child to the node and returns it for chaining.
func (n *Node) AddChild(child *Node) *Node {
	n.Children = append(n.Children, child)
	return child
}

// AddTextChild appends a text-only element in one call.
func (n *Node) AddTextChild(name, text string) *Node {
	return n.AddChild(&Node{Name: name, Text: text})
}

// Encode writes the node document to w with an XML declaration and UTF-8 encoding.
func Encode(w io.Writer, root *Node) error {
	if _, err := io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	if err := encodeNode(enc, root); err != nil {
		return err
	}
	return enc.Flush()
}

func encodeNode(enc *xml.Encoder, n *Node) error {
	start := xml.StartElement{Name: xml.Name{Local: n.Name}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if n.Text != "" {
		if err := enc.EncodeToken(xml.CharData(n.Text)); err != nil {
			return err
		}
	}
	for _, child := range n.Children {
		if err := encodeNode(enc, child); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}

// ErrorDocument builds the well-formed <error> document used for failures.
func ErrorDocument(code, message string) *Node {
	root := NewNode("error")
	root.AddTextChild("code", code)
	root.AddTextChild("message", message)
	return root
}

// ChannelListDocument builds <channel-playouts> with base fields (and, when
// enabled, the profile's channel custom fields) in deterministic order.
func ChannelListDocument(channels []ChannelView, fields []XMLField, includeMetadata bool) *Node {
	root := NewNode("channel-playouts")
	for _, ch := range channels {
		node := root.AddChild(NewNode("channel"))
		node.AddTextChild("id", ch.ID)
		node.AddTextChild("name", ch.Name)
		node.AddTextChild("img", ch.Img)
		appendCustomFields(node, fields, ch.Raw, includeMetadata)
	}
	return root
}

// EPGDocument builds <epg> with a channelId child and one <epgitem> per
// programme (base fields first, then the profile's EPG custom fields).
func EPGDocument(channelID string, items []EPGItemView, fields []XMLField, includeMetadata bool) *Node {
	root := NewNode("epg")
	root.AddTextChild("channelId", channelID)
	for _, it := range items {
		node := root.AddChild(NewNode("epgitem"))
		node.AddTextChild("start", it.Start)
		node.AddTextChild("stop", it.Stop)
		node.AddTextChild("streamId", it.StreamID)
		node.AddTextChild("ads", boolString(it.Ads))
		appendCustomFields(node, fields, it.Raw, includeMetadata)
	}
	return root
}

// appendCustomFields emits each valid custom field after the base elements.
// Fields with `when: metadata` are skipped unless includeMetadata is true.
func appendCustomFields(parent *Node, fields []XMLField, raw map[string]any, includeMetadata bool) {
	for _, f := range fields {
		if f.When == "metadata" && !includeMetadata {
			continue
		}
		text := ""
		if f.Value != "" {
			text = f.Value
		} else if v, ok := ResolveSourceString(raw, f.Source); ok {
			text = v
		} else {
			text = f.Default
		}
		parent.AddTextChild(f.Name, text)
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
