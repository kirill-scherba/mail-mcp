// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// parse.go — embedded Perl Email::MIME parser script.
// Called by mail_read tool to decode raw .eml files.
package main

// parsePerlScript is a Perl one-liner that reads a raw email from stdin
// and prints the decoded headers (From, To, Subject, Date) and body.
var parsePerlScript = `
use Email::MIME;
use Encode;

my $raw = do { local $/; <STDIN> };
my $email = Email::MIME->new($raw);

print "From: "    . $email->header('From')    . "\n";
print "To: "      . $email->header('To')      . "\n";
print "Subject: " . decode('MIME-Header', $email->header('Subject')) . "\n";
print "Date: "    . $email->header('Date')    . "\n";
print "---\n";

# Find text/plain part (preferred) or fallback to first part
for my $part ($email->parts) {
    my $ct = $part->content_type || '';
    if ($ct =~ m|text/plain|i) {
        print $part->body_str;
        last;
    }
}
if (!scalar($email->parts)) {
    print $email->body_str;
}
`
