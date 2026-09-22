package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
)

// Default phone pattern: an optional +, then 8..18 digits with separators.
const defaultPhonePattern = `\+?\d[\d\s\-().]{6,16}\d`

// tokenPrefix/tokenLen: a mask token is "<ph:" + 16 hex + ">" (21 bytes).
const (
	tokenPrefix = "<ph:"
	tokenSuffix = ">"
	tokenHexLen = 16
	tokenLen    = len(tokenPrefix) + tokenHexLen + len(tokenSuffix)
)

var tokenRe = regexp.MustCompile(`<ph:[0-9a-f]{16}>`)

// vault is the in-memory KV: hash → phone number. Entries live for the
// process lifetime (a real plugin would use a store with a TTL).
type vault struct {
	mu sync.RWMutex
	m  map[string]string
}

func newVault() *vault { return &vault{m: map[string]string{}} }

func (v *vault) put(phone string) string {
	sum := sha256.Sum256([]byte(phone))
	h := hex.EncodeToString(sum[:])[:tokenHexLen]
	v.mu.Lock()
	v.m[h] = phone
	v.mu.Unlock()
	return tokenPrefix + h + tokenSuffix
}

func (v *vault) get(token string) (string, bool) {
	h := strings.TrimSuffix(strings.TrimPrefix(token, tokenPrefix), tokenSuffix)
	v.mu.RLock()
	p, ok := v.m[h]
	v.mu.RUnlock()
	return p, ok
}

func (v *vault) size() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.m)
}

// mask replaces every phone number in s by a token and reports whether any was found.
func (v *vault) mask(re *regexp.Regexp, s string) (string, bool) {
	found := false
	out := re.ReplaceAllStringFunc(s, func(m string) string {
		found = true
		return v.put(m)
	})
	return out, found
}

// unmask restores every complete token in s.
func (v *vault) unmask(s string) (string, bool) {
	found := false
	out := tokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		if p, ok := v.get(tok); ok {
			found = true
			return p
		}
		return tok
	})
	return out, found
}

// splitTail splits s so that a possible token prefix at the end stays held back
// (it may complete in the next chunk); everything before it is safe to emit.
func splitTail(s string) (emit, hold string) {
	i := strings.LastIndex(s, "<")
	if i < 0 {
		return s, ""
	}
	tail := s[i:]
	if len(tail) >= tokenLen {
		return s, "" // a full-length tail is either a complete token or not one at all
	}
	if strings.HasPrefix(tokenPrefix, tail) || strings.HasPrefix(tail, tokenPrefix) {
		return s[:i], tail
	}
	return s, ""
}

// walkStrings applies fn to every string value of a decoded JSON tree in place.
func walkStrings(node any, fn func(string) string) any {
	switch x := node.(type) {
	case string:
		return fn(x)
	case []any:
		for i := range x {
			x[i] = walkStrings(x[i], fn)
		}
		return x
	case map[string]any:
		for k, v := range x {
			x[k] = walkStrings(v, fn)
		}
		return x
	}
	return node
}

// collectStrings gathers every string value of a decoded JSON tree.
func collectStrings(node any, out *[]string) {
	switch x := node.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, e := range x {
			collectStrings(e, out)
		}
	case map[string]any:
		for _, e := range x {
			collectStrings(e, out)
		}
	}
}
