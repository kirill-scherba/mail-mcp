// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// attachments.go — unpack the files of a letter without Perl.
//
// The Perl extractor needs Email::MIME, which is not installed everywhere mail-mcp runs: a container gets
// the binary but not the module, and a price list that arrives without its file is a letter without its
// reason (23.11.2026).
package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"strings"
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
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(header.Get("Content-Type"), ";")[0]))
	}
	decoded := decodeTransferEncoding(header.Get("Content-Transfer-Encoding"), body)

	if strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
		reader := multipart.NewReader(decoded, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				return
			}
			collectParts(mail.Header(part.Header), part, out)
		}
	}

	name := params["name"]
	if disposition, dispoParams, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		_ = disposition
		if f := dispoParams["filename"]; f != "" {
			name = f
		}
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		return
	}
	data, err := io.ReadAll(decoded)
	if err != nil {
		return
	}
	out[name] = data
}

// decodeTransferEncoding unwraps base64 or quoted-printable; anything else is already text.
func decodeTransferEncoding(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	default:
		return body
	}
}
