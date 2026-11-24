package web

import (
	"html/template"
	"strconv"
	"strings"
	"unicode/utf8"
)

// sgr is the text style that SGR sequences set; it maps to CSS classes.
type sgr struct {
	bold, dim, italic, underline bool
	fg, bg                       int // 0 is default; 1-16 are palette colors 0-15
}

func (s sgr) classes() string {
	var parts []string
	for _, flag := range []struct {
		on    bool
		class string
	}{{s.bold, "b"}, {s.dim, "d"}, {s.italic, "i"}, {s.underline, "u"}} {
		if flag.on {
			parts = append(parts, flag.class)
		}
	}
	if s.fg > 0 {
		parts = append(parts, "f"+strconv.Itoa(s.fg-1))
	}
	if s.bg > 0 {
		parts = append(parts, "g"+strconv.Itoa(s.bg-1))
	}
	return strings.Join(parts, " ")
}

// apply updates the style from one SGR parameter list. Colors beyond the 16
// palette entries are dropped rather than inlined, so the CSP needs no styles.
func (s *sgr) apply(params string) {
	codes := strings.Split(params, ";")
	for i := 0; i < len(codes); i++ {
		code, err := strconv.Atoi(codes[i])
		if err != nil {
			code = 0 // An empty parameter means reset.
		}
		switch {
		case code == 0:
			*s = sgr{}
		case code == 1:
			s.bold = true
		case code == 2:
			s.dim = true
		case code == 3:
			s.italic = true
		case code == 4:
			s.underline = true
		case code == 22:
			s.bold, s.dim = false, false
		case code == 23:
			s.italic = false
		case code == 24:
			s.underline = false
		case code >= 30 && code <= 37:
			s.fg = code - 29
		case code == 39:
			s.fg = 0
		case code >= 40 && code <= 47:
			s.bg = code - 39
		case code == 49:
			s.bg = 0
		case code >= 90 && code <= 97:
			s.fg = code - 81
		case code >= 100 && code <= 107:
			s.bg = code - 91
		case code == 38 || code == 48:
			color := 0
			if i+2 < len(codes) && codes[i+1] == "5" {
				if n, err := strconv.Atoi(codes[i+2]); err == nil && n < 16 {
					color = n + 1
				}
				i += 2
			} else if i+1 < len(codes) && codes[i+1] == "2" {
				i += 4
			}
			if code == 38 {
				s.fg = color
			} else {
				s.bg = color
			}
		}
	}
}

// renderANSI escapes one log line and maps SGR styles to classes. Every other
// escape sequence (cursor movement, OSC titles and hyperlinks) and every C0 and
// C1 control except tab is dropped, so logs cannot spoof links or the page.
func renderANSI(line []byte) template.HTML {
	var out strings.Builder
	var text strings.Builder
	var style sgr
	open := ""
	flush := func() {
		if text.Len() == 0 {
			return
		}
		if classes := style.classes(); classes != open {
			if open != "" {
				out.WriteString("</span>")
			}
			if classes != "" {
				out.WriteString(`<span class="` + classes + `">`)
			}
			open = classes
		}
		template.HTMLEscape(&out, []byte(text.String()))
		text.Reset()
	}
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRune(line[i:])
		switch {
		case r == 0x1b && i+1 < len(line) && line[i+1] == '[', r == 0x9b:
			start := i + size
			if r == 0x1b {
				start = i + 2
			}
			end := start
			for end < len(line) && (line[end] < 0x40 || line[end] > 0x7e) {
				end++
			}
			if end < len(line) && line[end] == 'm' {
				flush()
				style.apply(string(line[start:end]))
			}
			i = min(end+1, len(line))
			continue
		case r == 0x1b && i+1 < len(line) && line[i+1] == ']', r == 0x9d:
			// OSC runs to BEL or ST (ESC \ or U+009C).
			if r == 0x1b {
				i += 2
			} else {
				i += size
			}
			for i < len(line) {
				next, width := utf8.DecodeRune(line[i:])
				i += width
				if next == 0x07 || next == 0x9c {
					break
				}
				if next == 0x1b && i < len(line) && line[i] == '\\' {
					i++
					break
				}
			}
			continue
		case r == 0x1b:
			// Any other escape drops itself and its final byte.
			i += size
			if i < len(line) {
				i++
			}
			continue
		case r == '\t':
			text.WriteRune(r)
		case r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f:
		default:
			text.WriteRune(r)
		}
		i += size
	}
	flush()
	if open != "" {
		out.WriteString("</span>")
	}
	return template.HTML(out.String())
}
