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

// extractAttachPerlScript reads a raw email from a file (argv[0]),
// saves all attachments to a directory (argv[1]).
var extractAttachPerlScript = `
use strict;
use Email::MIME;
use File::Basename;
use MIME::Base64;

my ($file, $outdir) = @ARGV;
die "Usage: $0 <eml_file> <outdir>" unless $file && $outdir;

open(my $fh, '<:raw', $file) or die "open $file: $!";
my $raw = do { local $/; <$fh> };
close $fh;

my $email = Email::MIME->new($raw);
my $count = 0;

sub save_part {
    my ($part, $index) = @_;
    my $ct = $part->content_type || 'application/octet-stream';
    my $fn = $part->filename || "attachment_$index";
    $fn =~ s/[^\w.-]//g;
    $fn = "attachment_$index.dat" unless $fn;

    my $body = $part->body;
    open(my $out, '>:raw', "$outdir/$fn") or warn "save $fn: $!";
    print $out $body if $out;
    close $out if $out;
    $count++;
}

my $idx = 0;
for my $part ($email->parts) {
    my $ct = $part->content_type || '';
    if ($ct =~ m|multipart/|i) {
        for my $sub ($part->parts) {
            # Only save parts that have a filename (attachments)
            next unless $sub->filename;
            save_part($sub, $idx++);
        }
    } elsif ($part->filename) {
        save_part($part, $idx++);
    }
}

print "extracted $count attachment(s) to $outdir\n";
`
