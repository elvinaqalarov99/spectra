package inference

import (
	"net/url"
	"regexp"
	"strings"
	"sync"
)

var (
	reNumeric = regexp.MustCompile(`^\d+$`)
	reUUIDSeg = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reSlug    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{8,}[a-z0-9])?$`)

	// Tokens: hex strings ≥20 chars (SHA1=40, SHA256=64, MD5=32, short tokens ≥20)
	reHexToken = regexp.MustCompile(`(?i)^[0-9a-f]{20,}$`)

	// Bad sentinel values that should never appear in real API calls
	badSegments = map[string]bool{
		"null":      true,
		"undefined": true,
		"nan":       true,
		"0":         true, // ID 0 is never a real resource
		"xxx":       true, // common placeholder in tests/docs
		"test":      true,
		"example":   true,
	}
)

// trieNode builds a prefix-trie of path segments to detect parameter positions
type trieNode struct {
	children map[string]*trieNode
	count    int
}

// PathNormalizer accumulates path observations and produces normalized templates
type PathNormalizer struct {
	mu   sync.Mutex
	root *trieNode
}

func NewPathNormalizer() *PathNormalizer {
	return &PathNormalizer{root: &trieNode{children: map[string]*trieNode{}}}
}

// Observe records a raw URL path and returns the normalized template.
// Returns "" if the path should be skipped (contains null/undefined/etc).
func (n *PathNormalizer) Observe(rawPath string) string {
	// Strip query string and URL-decode (%7Buuid%7D → {uuid})
	rawPath = strings.Split(rawPath, "?")[0]
	if decoded, err := url.PathUnescape(rawPath); err == nil {
		rawPath = decoded
	}

	segments := splitPath(rawPath)

	// Skip paths with sentinel segments — frontend bug, not real endpoints
	for _, seg := range segments {
		if badSegments[strings.ToLower(seg)] {
			return ""
		}
		// Skip paths where a segment is literally a template placeholder
		// e.g. {uuid} was left unresolved by the client
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			// Replace it with {id} and return immediately — no need to record
			normalized := make([]string, len(segments))
			for i, s := range segments {
				if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
					normalized[i] = "{id}"
				} else {
					normalized[i] = s
				}
			}
			return "/" + strings.Join(normalized, "/")
		}
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	insertTrie(n.root, segments)
	markParams(n.root)
	template := buildTemplate(n.root, segments)

	return "/" + strings.Join(template, "/")
}

// NormalizedPaths returns all distinct path templates seen so far
func (n *PathNormalizer) NormalizedPaths() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	markParams(n.root)
	var paths []string
	collectPaths(n.root, nil, &paths)
	return paths
}

// ---- trie helpers ----

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return []string{}
	}
	return strings.Split(p, "/")
}

func insertTrie(node *trieNode, segments []string) {
	if len(segments) == 0 {
		node.count++
		return
	}
	seg := segments[0]
	child, ok := node.children[seg]
	if !ok {
		child = &trieNode{children: map[string]*trieNode{}}
		node.children[seg] = child
	}
	insertTrie(child, segments[1:])
}

// markParams collapses ID-like siblings into {id}.
// Tokens (hex hashes, etc.) are collapsed even with a single observation.
// Regular IDs (numeric, UUID) require ≥2 distinct values.
func markParams(node *trieNode) {
	if len(node.children) == 0 {
		return
	}

	// Recurse first
	for _, child := range node.children {
		markParams(child)
	}

	// Pass 1: collapse tokens immediately — single observation is enough
	// (hex hashes, SHA1 tokens, etc. are unambiguously dynamic)
	tokenCandidates := []string{}
	for seg := range node.children {
		if seg != "{id}" && looksLikeToken(seg) {
			tokenCandidates = append(tokenCandidates, seg)
		}
	}
	if len(tokenCandidates) > 0 {
		merged := &trieNode{children: map[string]*trieNode{}}
		for _, seg := range tokenCandidates {
			mergeTrieNodes(merged, node.children[seg])
			delete(node.children, seg)
		}
		if existing, ok := node.children["{id}"]; ok {
			mergeTrieNodes(existing, merged)
		} else {
			node.children["{id}"] = merged
		}
	}

	// Pass 2: collapse numeric / UUID IDs when ≥2 distinct values seen
	paramCandidates := []string{}
	for seg := range node.children {
		if seg != "{id}" && looksLikeID(seg) {
			paramCandidates = append(paramCandidates, seg)
		}
	}
	if len(paramCandidates) >= 2 {
		merged := &trieNode{children: map[string]*trieNode{}}
		for _, seg := range paramCandidates {
			mergeTrieNodes(merged, node.children[seg])
			delete(node.children, seg)
		}
		if existing, ok := node.children["{id}"]; ok {
			mergeTrieNodes(existing, merged)
		} else {
			node.children["{id}"] = merged
		}
	}
}

func mergeTrieNodes(dst, src *trieNode) {
	dst.count += src.count
	for k, v := range src.children {
		if existing, ok := dst.children[k]; ok {
			mergeTrieNodes(existing, v)
		} else {
			dst.children[k] = v
		}
	}
}

// looksLikeID: UUID — needs ≥2 siblings to collapse (could be a named resource).
func looksLikeID(s string) bool {
	return reUUIDSeg.MatchString(s)
}

// looksLikeToken: auto-collapsed on first observation — unambiguously dynamic.
// - Pure integers: no REST API has a static integer path segment
// - Hex tokens ≥20 chars: SHA1/SHA256/MD5 hashes
// - Long slugs ≥10 chars: opaque identifiers
func looksLikeToken(s string) bool {
	if reNumeric.MatchString(s) {
		return true // integers are always dynamic IDs
	}
	if len(s) < 10 {
		return false
	}
	return reHexToken.MatchString(s) || reSlug.MatchString(s)
}

func buildTemplate(node *trieNode, segments []string) []string {
	if len(segments) == 0 {
		return []string{}
	}
	seg := segments[0]
	rest := segments[1:]

	if _, ok := node.children["{id}"]; ok {
		if looksLikeToken(seg) || looksLikeID(seg) {
			return append([]string{"{id}"}, buildTemplate(node.children["{id}"], rest)...)
		}
	}
	if child, ok := node.children[seg]; ok {
		return append([]string{seg}, buildTemplate(child, rest)...)
	}
	return append([]string{seg}, rest...)
}

func collectPaths(node *trieNode, prefix []string, out *[]string) {
	if node.count > 0 || len(node.children) == 0 {
		path := "/" + strings.Join(prefix, "/")
		*out = append(*out, path)
	}
	for seg, child := range node.children {
		collectPaths(child, append(prefix, seg), out)
	}
}
