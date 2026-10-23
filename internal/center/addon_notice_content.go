package center

// Restrict the fixed MiSub announcement markup to a data tree. No raw HTML is
// sent to an HTML sink; React creates only these explicit elements/attributes.
import (
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type noticeNode struct {
	Tag      string       `json:"tag,omitempty"`
	Text     string       `json:"text,omitempty"`
	Href     string       `json:"href,omitempty"`
	Children []noticeNode `json:"children,omitempty"`
}

var noticeRawTag = regexp.MustCompile(`^</?[A-Za-z][A-Za-z0-9]*`)
var noticeRawAttribute = regexp.MustCompile(`^\s+([A-Za-z][A-Za-z0-9_-]*)\s*=\s*(?:"[^"]*"|'[^']*')`)

func validateNoticeRawTag(raw string) error {
	prefix := noticeRawTag.FindString(raw)
	if prefix == "" {
		return errors.New("invalid_notice_markup")
	}
	rest := raw[len(prefix):]
	seen := map[string]bool{}
	for {
		if tail := strings.TrimSpace(rest); tail == ">" || tail == "/>" {
			return nil
		}
		match := noticeRawAttribute.FindStringSubmatch(rest)
		if match == nil {
			return errors.New("invalid_notice_attribute")
		}
		key := strings.ToLower(match[1])
		if seen[key] {
			return errors.New("duplicate_notice_attribute")
		}
		seen[key] = true
		rest = rest[len(match[0]):]
	}
}

func parseNoticeContent(content, format string) ([]noticeNode, error) {
	if format == "" || format == "text" {
		return nil, nil
	}
	if format != "html" || len(content) > 8192 {
		return nil, errors.New("invalid_notice_markup")
	}
	// HTML tree construction drops duplicate attributes and container tags.
	// Reject these in the original token stream before parsing the structure.
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, errors.New("invalid_notice_markup")
			}
			break
		}
		if kind == html.CommentToken || kind == html.DoctypeToken {
			return nil, errors.New("unsupported_notice_tag")
		}
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken || kind == html.EndTagToken {
			if e := validateNoticeRawTag(string(z.Raw())); e != nil {
				return nil, e
			}
			token := z.Token()
			if !catalogChoice(token.Data, "b", "i", "em", "strong", "a", "p", "br", "ul", "li", "code", "pre") {
				return nil, errors.New("unsupported_notice_tag")
			}
			seen := map[string]bool{}
			for _, attribute := range token.Attr {
				if seen[attribute.Key] {
					return nil, errors.New("duplicate_notice_attribute")
				}
				seen[attribute.Key] = true
			}
		}
	}
	roots, e := html.ParseFragment(strings.NewReader(content), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if e != nil {
		return nil, errors.New("invalid_notice_markup")
	}
	count := 0
	var walk func(*html.Node, int) (noticeNode, error)
	walk = func(n *html.Node, depth int) (noticeNode, error) {
		count++
		if count > 256 || depth > 12 {
			return noticeNode{}, errors.New("notice_markup_limit")
		}
		if n.Type == html.TextNode {
			return noticeNode{Text: n.Data}, nil
		}
		if n.Type != html.ElementNode || n.Namespace != "" || !catalogChoice(n.Data, "b", "i", "em", "strong", "a", "p", "br", "ul", "li", "code", "pre") {
			return noticeNode{}, errors.New("unsupported_notice_tag")
		}
		v := noticeNode{Tag: n.Data}
		seen := map[string]bool{}
		for _, a := range n.Attr {
			if n.Data != "a" || a.Namespace != "" || seen[a.Key] || !catalogChoice(a.Key, "href", "target", "rel") {
				return noticeNode{}, errors.New("unsupported_notice_attribute")
			}
			seen[a.Key] = true
			if a.Key == "href" {
				u, e := url.Parse(a.Val)
				if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(a.Val, "\r\n\t\\") || len(a.Val) > 2000 {
					return noticeNode{}, errors.New("unsafe_notice_link")
				}
				v.Href = u.String()
			}
			if a.Key == "target" && !catalogChoice(a.Val, "_blank", "_self") {
				return noticeNode{}, errors.New("unsafe_notice_target")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			child, e := walk(c, depth+1)
			if e != nil {
				return noticeNode{}, e
			}
			v.Children = append(v.Children, child)
		}
		return v, nil
	}
	result := []noticeNode{}
	for _, r := range roots {
		v, e := walk(r, 0)
		if e != nil {
			return nil, e
		}
		result = append(result, v)
	}
	return result, nil
}
