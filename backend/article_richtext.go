package main

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxArticleHTMLBytes = 100 << 10
const maxArticleImages = 30

var articleIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
var durationRE = regexp.MustCompile(`^(?:[0-9]{1,3}:[0-5][0-9]:[0-5][0-9]|[0-9]{1,4}:[0-5][0-9])$`)

// No URL is fetched here: browser decoding checks playback without exposing
// the backend to requests to arbitrary user-supplied addresses.
func validateContentURL(value string, allowCloud bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, `\<>"`) || strings.IndexFunc(value, unicode.IsSpace) >= 0 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("地址格式无效")
	}
	u, err := url.Parse(value)
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return "", errors.New("请输入完整的 HTTPS 地址")
	}
	if allowCloud && u.Scheme == "cloud" && u.Path != "" && u.RawQuery == "" && u.Fragment == "" {
		return value, nil
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("地址端口无效")
		}
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("请使用 HTTPS 地址；本地开发允许 localhost HTTP")
	}
	return value, nil
}

func validateArticle(a Article) (Article, error) {
	a.ID = strings.TrimSpace(a.ID)
	if a.ID != "" && !articleIDRE.MatchString(a.ID) {
		return a, errors.New("文章 ID 无效")
	}
	for _, field := range []struct {
		name  string
		value *string
		limit int
	}{{"标题", &a.Title, 120}, {"摘要", &a.Summary, 260}, {"品种", &a.Variety, 50}, {"种植阶段", &a.Stage, 50}} {
		*field.value = strings.TrimSpace(*field.value)
		if *field.value == "" || utf8.RuneCountInString(*field.value) > field.limit {
			return a, fmt.Errorf("文章%s不能为空且不得超过 %d 字", field.name, field.limit)
		}
		*field.value = clean(*field.value, field.limit)
		if *field.value == "" {
			return a, fmt.Errorf("文章%s不能为空", field.name)
		}
	}
	var err error
	a.Cover, err = validateContentURL(a.Cover, true)
	if err != nil {
		return a, fmt.Errorf("封面%s", err.Error())
	}
	if a.Type == "" {
		a.Type = "article"
	}
	if a.Type != "article" && a.Type != "video" {
		return a, errors.New("内容类型无效")
	}
	if a.Status == "" {
		a.Status = "PUBLISHED"
	}
	if a.Status != "PUBLISHED" && a.Status != "DRAFT" && a.Status != "SCHEDULED" {
		return a, errors.New("内容状态无效")
	}
	if a.Type == "video" {
		a.VideoURL, err = validateContentURL(a.VideoURL, false)
		if err != nil {
			return a, fmt.Errorf("视频%s；cloud:// fileID 请换为可播放的 HTTPS 地址", err.Error())
		}
		a.Duration = strings.TrimSpace(a.Duration)
		if a.Duration != "" && !durationRE.MatchString(a.Duration) {
			return a, errors.New("视频时长格式应为分:秒或时:分:秒")
		}
	} else {
		a.VideoURL, a.Duration = "", ""
	}
	if a.PublishedAt != "" {
		valid := false
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
			if t, e := time.Parse(layout, a.PublishedAt); e == nil && t.Year() >= 1000 {
				valid = true
				break
			}
		}
		if !valid {
			return a, errors.New("发布日期无效")
		}
	}
	if strings.TrimSpace(a.ContentHTML) == "" {
		var body strings.Builder
		for _, paragraph := range a.Content {
			paragraph = strings.TrimSpace(paragraph)
			if paragraph != "" {
				body.WriteString("<p>" + html.EscapeString(paragraph) + "</p>")
			}
			if body.Len() > maxArticleHTMLBytes {
				return a, errors.New("正文不得超过 100KB")
			}
		}
		a.ContentHTML = body.String()
	}
	a.ContentHTML, a.Content, err = sanitizeArticleHTML(a.ContentHTML)
	return a, err
}

var articleTags = map[string]string{
	"p": "p", "div": "p", "br": "br", "h1": "h2", "h2": "h2", "h3": "h3", "h4": "h3",
	"strong": "strong", "b": "strong", "em": "em", "i": "em", "u": "u", "s": "s", "strike": "s",
	"ul": "ul", "ol": "ol", "li": "li", "blockquote": "blockquote", "a": "a", "img": "img", "hr": "hr", "span": "span",
}
var articleDropTags = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true, "svg": true, "math": true,
	"template": true, "form": true, "input": true, "button": true, "textarea": true, "select": true,
	"audio": true, "video": true, "canvas": true, "link": true, "meta": true, "base": true, "noscript": true,
}

// The same fixed styles are used by the admin preview and WeChat rich-text.
// Client CSS is discarded rather than copied into readers' rendering context.
var articleStyles = map[string]string{
	"p":          "margin:0 0 16px;line-height:1.8;word-break:break-word",
	"h2":         "margin:24px 0 12px;font-size:22px;font-weight:700;line-height:1.5",
	"h3":         "margin:20px 0 10px;font-size:18px;font-weight:700;line-height:1.5",
	"blockquote": "margin:16px 0;padding:10px 16px;border-left:3px solid #8b5c9f;color:#62566b;background:#f8f5fa",
	"ul":         "margin:12px 0;padding-left:24px;list-style-type:disc;line-height:1.8",
	"ol":         "margin:12px 0;padding-left:24px;list-style-type:decimal;line-height:1.8",
	"li":         "margin:6px 0;line-height:1.8",
	"img":        "display:block;max-width:100%;height:auto;margin:16px auto;border-radius:8px",
	"a":          "color:#765187;text-decoration:underline;word-break:break-all",
	"strong":     "font-weight:700", "em": "font-style:italic", "u": "text-decoration:underline", "s": "text-decoration:line-through",
}

// Parse the browser's HTML tree and rebuild from allowlists. Malformed tags,
// namespace tricks, entity-encoded URLs and event attributes cannot bypass it.
func sanitizeArticleHTML(source string) (string, []string, error) {
	if len(source) > maxArticleHTMLBytes || !utf8.ValidString(source) {
		return "", nil, errors.New("正文必须为有效文本且不得超过 100KB")
	}
	context := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := xhtml.ParseFragment(strings.NewReader(source), context)
	if err != nil {
		return "", nil, errors.New("正文格式无效")
	}
	root := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	images := 0
	var rebuild func(*xhtml.Node, *xhtml.Node, int) error
	rebuild = func(n, parent *xhtml.Node, depth int) error {
		if depth > 64 {
			return errors.New("正文嵌套层级过多")
		}
		if n.Type == xhtml.TextNode {
			parent.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: strings.ReplaceAll(n.Data, "\x00", "")})
			return nil
		}
		if n.Type != xhtml.ElementNode || n.Namespace != "" || articleDropTags[n.Data] {
			return nil
		}
		tag := articleTags[n.Data]
		target := parent
		if tag != "" {
			out := &xhtml.Node{Type: xhtml.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
			attrs := map[string]string{}
			for _, attr := range n.Attr {
				if attr.Namespace == "" {
					attrs[attr.Key] = attr.Val
				}
			}
			if tag == "img" {
				src, e := validateContentURL(attrs["src"], false)
				if e != nil {
					return nil
				}
				images++
				if images > maxArticleImages {
					return errors.New("正文最多包含 30 张图片")
				}
				out.Attr = append(out.Attr, xhtml.Attribute{Key: "src", Val: src})
				if alt := clean(attrs["alt"], 120); alt != "" {
					out.Attr = append(out.Attr, xhtml.Attribute{Key: "alt", Val: alt})
				}
			} else if tag == "a" {
				if href, e := validateContentURL(attrs["href"], false); e == nil {
					out.Attr = append(out.Attr, xhtml.Attribute{Key: "href", Val: href})
				}
			}
			if style := articleStyles[tag]; style != "" {
				out.Attr = append(out.Attr, xhtml.Attribute{Key: "style", Val: style})
			}
			parent.AppendChild(out)
			target = out
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := rebuild(child, target, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, n := range nodes {
		if err := rebuild(n, root, 0); err != nil {
			return "", nil, err
		}
	}
	var result strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if err := xhtml.Render(&result, child); err != nil {
			return "", nil, errors.New("正文格式无效")
		}
	}
	if result.Len() > maxArticleHTMLBytes {
		return "", nil, errors.New("正文不得超过 100KB")
	}
	paragraphs := articlePlainText(root)
	if len(paragraphs) == 0 && images == 0 {
		return "", nil, errors.New("正文不能为空，请输入文字或添加图片")
	}
	return result.String(), paragraphs, nil
}

func articlePlainText(root *xhtml.Node) []string {
	paragraphs := []string{}
	var text strings.Builder
	flush := func() {
		value := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.In(r, unicode.Cf) || unicode.IsControl(r) && !unicode.IsSpace(r) {
				return -1
			}
			return r
		}, text.String()))
		if value != "" {
			paragraphs = append(paragraphs, value)
		}
		text.Reset()
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		block := n.Data == "p" || n.Data == "h2" || n.Data == "h3" || n.Data == "li" || n.Data == "blockquote" || n.Data == "ul" || n.Data == "ol" || n.Data == "br" || n.Data == "hr"
		if n.Type == xhtml.ElementNode && block {
			flush()
		}
		if n.Type == xhtml.TextNode {
			text.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == xhtml.ElementNode && block {
			flush()
		}
	}
	walk(root)
	flush()
	return paragraphs
}
