package main

import (
	"strings"
	"testing"
)

// foldedName is a letter whose file name is an encoded word split across two lines, as supplier letters
// carry it (28.11.2026).
const foldedName = "From: a@b\r\n" +
	"To: c@d\r\n" +
	"Subject: price\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"B\"\r\n" +
	"\r\n" +
	"--B\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"see attached\r\n" +
	"--B\r\n" +
	"Content-Type: application/vnd.ms-excel.sheet.macroenabled.12;\r\n" +
	"\tname=\"=?UTF-8?B?0J/RgNCw0LnRgS3Qu9C40YHRgiBTVEFMRVg=?=\r\n" +
	" =?UTF-8?B?IDE5LjExLjIwMjYueGxzeA==?=\"\r\n" +
	"Content-Disposition: attachment;\r\n" +
	"\tfilename=\"=?UTF-8?B?0J/RgNCw0LnRgS3Qu9C40YHRgiBTVEFMRVg=?=\r\n" +
	" =?UTF-8?B?IDE5LjExLjIwMjYueGxzeA==?=\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"0J/RgNC40LLQtdGC\r\n" +
	"--B--\r\n"

func TestFoldedEncodedFileName(t *testing.T) {
	files := extractAttachmentsGo([]byte(foldedName))
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1: %v", len(files), keys(files))
	}
	want := "Прайс-лист STALEX 19.11.2026.xlsx"
	if _, ok := files[want]; !ok {
		t.Fatalf("name = %v, want %q", keys(files), want)
	}
	if got := strings.TrimSpace(string(files[want])); got != "Привет" {
		t.Fatalf("content = %q, want %q", got, "Привет")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
