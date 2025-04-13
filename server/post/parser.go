package post

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/markusmobius/go-dateparser"
)

// Post metadata after parsing. If the source changes, this must be reparsed
type Post struct {
	HTML    []byte
	Title   string
	Tags    map[string]struct{}
	Created time.Time
}

// Loads and parses the post
func loadPost(file string) (Post, error) {
	var p Post
	var root ast.Node

	// Read in the file
	if data, err := os.ReadFile(file); err != nil {
		return p, err
	} else {
		p := parser.New()
		p.Opts.ParserHook = parserHook
		root = markdown.Parse(data, p)
	}

	// Add in the other metadata
	ast.WalkFunc(root, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		switch v := node.(type) {
		case *ast.Text:
			if h, ok := v.Parent.(*ast.Heading); ok && h.Level == 1 && len(p.Title) == 0 {
				p.Title = string(v.Literal)
			}
		case *markdownDate:
			p.Created = v.date
		case *markdownTags:
			p.Tags = v.tags
		}
		return ast.GoToNext
	})

	// Render the HTML
	opts := html.RendererOptions{
		Flags:          html.CommonFlags | html.HrefTargetBlank,
		RenderNodeHook: renderHook,
	}
	renderer := html.NewRenderer(opts)
	p.HTML = markdown.Render(root, renderer)
	return p, nil
}

type markdownDate struct {
	ast.Leaf
	date time.Time
}

func parserHook(data []byte) (ast.Node, []byte, int) {
	if date, inner, size := markdownDateParser(data); date != nil {
		return date, inner, size
	} else if tags, inner, size := markdownTagsParser(data); tags != nil {
		return tags, inner, size
	} else {
		return nil, nil, 0
	}
}

func renderHook(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	switch v := node.(type) {
	case *markdownDate:
		markdownDateRender(w, v, entering)
		return ast.GoToNext, true
	case *markdownTags:
		markdownTagsRender(w, v, entering)
		return ast.GoToNext, true
	default:
		return ast.GoToNext, false
	}
}

func skipSpaces(data *[]byte) int {
	// Get n amount of spaces
	i := 0
	for (*data)[0] == ' ' {
		*data = (*data)[1:]
		i++
	}
	return i
}

func markdownCustomPrefix(data []byte) int {
	// See if its the start of a list item
	i := skipSpaces(&data)
	if !bytes.HasPrefix(data, []byte("- ")) {
		return 0
	} else {
		return i + 2
	}
}

func markdownDateParser(data []byte) (ast.Node, []byte, int) {
	// Consume the prefix
	i := markdownCustomPrefix(data)
	if i == 0 {
		return nil, nil, 0
	} else {
		data = data[i:]
	}

	// Parse the date
	var str []byte
	if i := bytes.Index(data, []byte("\n")); i == -1 {
		str = data
	} else {
		str = data[0:i]
	}
	if date, err := dateparser.Parse(nil, string(str)); err != nil {
		return nil, nil, 0
	} else {
		return &markdownDate{date: date.Time}, nil, i + len(str)
	}
}

func markdownDateRender(w io.Writer, date *markdownDate, _ bool) {
	io.WriteString(w, "<em class=\"date\">")
	fmt.Fprintf(w, "%s %d %d", date.date.Month().String(), date.date.Day(), date.date.Year())
	io.WriteString(w, "</em>\n")
}

type markdownTags struct {
	ast.Leaf
	tags map[string]struct{}
}

func markdownTagsParser(data []byte) (ast.Node, []byte, int) {
	// Consume the prefix
	i := markdownCustomPrefix(data)
	if i == 0 {
		return nil, nil, 0
	} else {
		data = data[i:]
	}

	// Then consume the tags (if there are any)
	tags := make(map[string]struct{})
	for {
		i += skipSpaces(&data)
		if data[0] != '#' {
			break
		} else {
			data = data[1:]
			i++
		}

		var tag strings.Builder
		for r, size := utf8.DecodeRune(data); unicode.IsLetter(r) || r == '_'; r, size = utf8.DecodeRune(data) {
			tag.WriteRune(r)
			data = data[size:]
			i += size
		}
		tags[tag.String()] = struct{}{}
	}
	if len(tags) == 0 {
		return nil, nil, 0
	}

	return &markdownTags{tags: tags}, nil, i
}

func markdownTagsRender(w io.Writer, tags *markdownTags, _ bool) {
	io.WriteString(w, "<ul class=\"tags\">\n")
	for tag := range tags.tags {
		fmt.Fprintf(w, "  <li>%s</li>\n", tag)
	}
	io.WriteString(w, "</ul>\n")
}
