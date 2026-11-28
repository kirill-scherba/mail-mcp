// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// attachments.go — unpack the files of a letter without Perl.
//
// The Perl extractor needs Email::MIME, which is not installed everywhere mail-mcp runs: a container gets
// the binary but not the module, and a price list that arrives without its file is a letter without its
// reason (23.11.2026).
//
// A file name is the hard part. Real letters from suppliers carry it as an RFC 2047 encoded word, often
// folded across two lines with a space at the fold:
//
//	Content-Disposition: attachment; filename="=?UTF-8?B?0J/RgNCw0LnRgS3Qu9C40YHRgiBTVEFMRVg=?=
//	 =?UTF-8?B?IDE5LjExLjIwMjYueGxzeA==?="
//
// mime.ParseMediaType stops at the fold and answers the tail of the word, so the name arrived as
// "QsNC8Lnhscw==?=" and the file lost its extension. Unfolding first and decoding the words afterwards is
// what a person reading the letter does (28.11.2026).
package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// extractAttachmentsGo answers the named parts of a message: file name → content.
func extractAttachmentsGo(raw []byte) map[string][]byte {
	out := map[string][]byte{}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return out
	}
	collectParts(msg.Header, msg.Body, out)
	return out
}

// collectParts walks a MIME tree, decoding each part on the way.
func collectParts(header mail.Header, body io.Reader, out map[string][]byte) {
	mediaType, params, err := mime.ParseMediaType(unfoldHeader(header.Get("Content-Type")))
	if err != nil || mediaType == "" {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(header.Get("Content-Type"), ";")[0]))
	}

	if strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
		reader := multipart.NewReader(body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				return
			}
			collectParts(mail.Header(part.Header), part, out)
		}
	}

	name := attachmentName(header)
	if name == "" {
		return
	}
	// A part whose base64 ends in the middle of a quantum still holds everything before that: keep what was
	// read instead of throwing the file away (28.11.2026).
	data, _ := io.ReadAll(decodeTransferEncoding(header.Get("Content-Transfer-Encoding"), body))
	out[name] = data
}

// attachmentName answers the name of a part, unfolding the header and decoding its encoded words.
func attachmentName(header mail.Header) string {
	for _, key := range []string{"Content-Disposition", "Content-Type"} {
		// A letter that passed through a receiver which decoded it as quoted-printable lost the "=" of a word
		// that ended a line: "?=" + break became "? ". Put the character back; without it the name stays raw
		// and the file arrives without its extension (28.11.2026).
		value := strings.ReplaceAll(unfoldHeader(header.Get(key)), "? =?", "?==?")
		if value == "" {
			continue
		}
		if _, params, err := mime.ParseMediaType(value); err == nil {
			for _, param := range []string{"filename", "name"} {
				if name := cleanFileName(params[param]); name != "" {
					return name
				}
			}
		}
		for _, param := range []string{"filename", "name"} {
			if name := cleanFileName(rawParam(value, param)); name != "" {
				return name
			}
		}
	}
	return ""
}

// unfoldHeader joins the lines of a folded header and squeezes the breaks to one space. A fold may be a tab,
// and an encoded word separated from its neighbour by a tab is not an encoded word any more - the name then
// stays raw (28.11.2026).
func unfoldHeader(value string) string {
	value = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

// rawParam answers a parameter from a header value as it is written, quotes and all.
func rawParam(value, name string) string {
	lower := strings.ToLower(value)
	for _, prefix := range []string{";" + name + "=", ";" + name + " =", ";" + name + "=  "} {
		if i := strings.Index(lower, prefix); i >= 0 {
			rest := strings.TrimSpace(value[i+len(prefix):])
			if strings.HasPrefix(rest, `"`) {
				if j := strings.Index(rest[1:], `"`); j >= 0 {
					return rest[1 : 1+j]
				}
				return rest[1:]
			}
			if j := strings.Index(rest, ";"); j >= 0 {
				return rest[:j]
			}
			return rest
		}
	}
	return ""
}

// cleanFileName decodes encoded words, drops any path and refuses a name that is not a name.
func cleanFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = decodeWords(name)
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	name = filepath.Base(strings.TrimSpace(name))
	// An empty or one-character name has lost its content on the way and says nothing about the file.
	if len([]rune(name)) < 2 || name == "." || name == ".." {
		return ""
	}
	return name
}

// encodedWord matches one RFC 2047 word, base64 or quoted-printable.
var encodedWord = regexp.MustCompile(`=\?[^?\s]+\?[bBqQ]\?[^?\s]*\?=`)

// decodeWords decodes the encoded words of a name one by one. mime.WordDecoder.DecodeHeader leaves a value raw
// when two words stand inside quotes and are separated by a space - which is exactly how suppliers write a
// long file name - so the words are taken separately and the whitespace between two of them is dropped
// (28.11.2026).
func decodeWords(value string) string {
	var out strings.Builder
	last, afterWord := 0, false
	for _, m := range encodedWord.FindAllStringIndex(value, -1) {
		between := value[last:m[0]]
		// Text between words is kept; whitespace between two encoded words is not text.
		if !afterWord || strings.TrimSpace(between) != "" {
			out.WriteString(between)
		}
		word := value[m[0]:m[1]]
		if decoded, err := new(mime.WordDecoder).Decode(word); err == nil {
			out.WriteString(decoded)
		} else {
			out.WriteString(word)
		}
		last, afterWord = m[1], true
	}
	out.WriteString(value[last:])
	return out.String()
}

// runReattach unpacks the files of the named mailboxes again, replacing what an older extractor wrote.
func runReattach(mailboxes []string) {
	if len(mailboxes) == 0 {
		for _, box := range mailBoxes() {
			mailboxes = append(mailboxes, box.Address)
		}
	}
	for _, mailbox := range mailboxes {
		dir := mailboxDir(mailbox)
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("attachments %s: %v", mailbox, err)
			continue
		}
		done := 0
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".eml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				log.Printf("attachments %s/%s: %v", mailbox, e.Name(), err)
				continue
			}
			os.RemoveAll(filepath.Join(dir, "attachments", e.Name()))
			if names := ensureAttachments(dir, e.Name(), raw); len(names) > 0 {
				done++
			}
		}
		log.Printf("attachments %s: писем с файлами: %d", mailbox, done)
	}
}

// decodeTransferEncoding unwraps base64 or quoted-printable; anything else is already text.
func decodeTransferEncoding(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return bytes.NewReader(decodeBase64(body))
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	default:
		return body
	}
}

// decodeBase64 reads a base64 part and decodes as much of it as it can. The stream decoder stops at the first
// incomplete quantum and loses the bytes before it, while a damaged letter usually keeps a whole file minus
// its last few characters - so the text is collected first, whitespace dropped and the padding restored
// (28.11.2026).
func decodeBase64(body io.Reader) []byte {
	text, err := io.ReadAll(body)
	if err != nil {
		text = text[:0]
	}
	clean := make([]byte, 0, len(text))
	for _, b := range text {
		if b == '\r' || b == '\n' || b == ' ' || b == '\t' {
			continue
		}
		clean = append(clean, b)
	}
	if rest := len(clean) % 4; rest != 0 {
		clean = append(clean, bytes.Repeat([]byte{'='}, 4-rest)...)
	}
	data, err := base64.StdEncoding.DecodeString(string(clean))
	if err == nil {
		return data
	}
	// Still broken: keep what decodes up to the first bad character.
	good := 0
	for best := len(clean) - 4; best > 0; best -= 4 {
		if part, err := base64.StdEncoding.DecodeString(string(clean[:best]) + strings.Repeat("=", 0)); err == nil {
			good = len(part)
			_ = good
			return part
		}
	}
	return data
}
