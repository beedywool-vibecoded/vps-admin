package proxy

import (
	"sort"
	"strings"
)

// A site written in a shared configuration file (the main Caddyfile, or a file imported
// from it) cannot be paused by moving the file aside: the other sites would go with it.
// The block alone is commented out between these markers. Caddy ignores comments, the
// panel lists the block as paused, and restoring it puts the exact lines back.
const (
	PausedBegin = "# vps-admin: paused site"
	PausedEnd   = "# vps-admin: end paused site"
)

// CommentBlock takes the site block serving domain out of src, as comments between the
// two markers. A leading comment attached to the block travels with it. It reports false
// when the file has no such block.
func CommentBlock(src, domain string) (string, bool) {
	lines := strings.Split(src, "\n")
	start, end, ok := findBlock(lines, domain)
	if !ok {
		return src, false
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:start]...)
	out = append(out, PausedBegin)
	for _, line := range lines[start : end+1] {
		out = append(out, "#"+line)
	}
	out = append(out, PausedEnd)
	out = append(out, lines[end+1:]...)
	return strings.Join(out, "\n"), true
}

// RestoreBlock puts the paused block serving domain back in place, exactly as it was
// written. It reports false when no paused block serves that domain.
func RestoreBlock(src, domain string) (string, bool) {
	lines := strings.Split(src, "\n")
	for _, span := range pausedSpans(lines) {
		if !serves(span.body, domain) {
			continue
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:span.start]...)
		out = append(out, span.body...)
		out = append(out, lines[span.end+1:]...)
		return strings.Join(out, "\n"), true
	}
	return src, false
}

// RemoveBlock deletes every block serving domain from src, paused or active. A deleted
// site is not kept aside: the caller reloads the proxy after the file is written.
func RemoveBlock(src, domain string) (string, bool) {
	lines := strings.Split(src, "\n")
	var spans []blockSpan
	if start, end, ok := findBlock(lines, domain); ok {
		spans = append(spans, blockSpan{start: start, end: end})
	}
	for _, span := range pausedSpans(lines) {
		if serves(span.body, domain) {
			spans = append(spans, span)
		}
	}
	if len(spans) == 0 {
		return src, false
	}
	// Remove from the bottom up so the indexes above stay valid.
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	for _, span := range spans {
		lines = append(lines[:span.start], lines[span.end+1:]...)
	}
	return strings.Join(lines, "\n"), true
}

// pausedBlockSites returns the site blocks kept as comments by CommentBlock. They are
// listed with their real file name, so the panel can offer to turn them back on.
func pausedBlockSites(src, file, apps string) []Site {
	var out []Site
	for _, span := range pausedSpans(strings.Split(src, "\n")) {
		sites, _ := parseCaddy(strings.Join(span.body, "\n"), file, apps)
		for _, site := range sites {
			if len(site.Domains) == 0 {
				continue
			}
			site.Paused = true
			out = append(out, site)
		}
	}
	return out
}

type blockSpan struct {
	start int
	end   int
	body  []string
}

// pausedSpans finds the marker pairs and returns the uncommented lines between them.
// A marker that is not closed, or that wraps lines not commented out, is ignored.
func pausedSpans(lines []string) []blockSpan {
	var out []blockSpan
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != PausedBegin {
			continue
		}
		body := []string{}
		bad := false
		j := i + 1
		for ; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == PausedEnd {
				break
			}
			line := lines[j]
			// CommentBlock prefixes every line with one #; strip exactly that one so
			// comments inside the block come back unchanged.
			if !strings.HasPrefix(line, "#") {
				bad = true
				break
			}
			body = append(body, line[1:])
		}
		if j >= len(lines) || bad {
			break
		}
		out = append(out, blockSpan{start: i, end: j, body: body})
		i = j
	}
	return out
}

// findBlock locates the top-level site block whose address list contains domain.
func findBlock(lines []string, domain string) (start, end int, ok bool) {
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(comment(lines[i]))
		if !strings.HasSuffix(line, "{") {
			continue
		}
		blockEnd, closed := blockEnd(lines, i)
		if !closed {
			return 0, 0, false
		}
		head := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		domains, _ := caddyAddresses(head)
		if hasDomain(domains, domain) {
			start = i
			// A comment directly above describes the site: take it along, but never
			// swallow the markers of another paused block.
			for start > 0 {
				prev := strings.TrimSpace(lines[start-1])
				if prev == "" || !strings.HasPrefix(prev, "#") || prev == PausedBegin || prev == PausedEnd {
					break
				}
				start--
			}
			return start, blockEnd, true
		}
		i = blockEnd
	}
	return 0, 0, false
}

// blockEnd returns the line closing the block that opens at start.
func blockEnd(lines []string, start int) (int, bool) {
	depth := 0
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(comment(lines[i]))
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "{") {
			depth++
		}
		if strings.HasPrefix(line, "}") {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func serves(body []string, domain string) bool {
	sites, _ := parseCaddy(strings.Join(body, "\n"), "", "")
	for _, site := range sites {
		if hasDomain(site.Domains, domain) {
			return true
		}
	}
	return false
}

func hasDomain(domains []string, domain string) bool {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return false
	}
	for _, d := range domains {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}
