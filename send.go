// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package main

// sendPerlScript sends email via Brevo SMTP relay using Perl.
// Uses Net::SMTP with STARTTLS. No Python involved.
var sendPerlScript = `
use strict;
use Net::SMTP;

my ($from_name, $from_addr, $to, $subject, $in_reply_to) = @ARGV;
die "Missing args" unless $to && $subject;

# Read body from stdin (to avoid ARG_MAX limits)
my $body = do { local $/; <STDIN> };
die "Empty body" unless $body;

$from_name = "Барон" unless $from_name;
$from_addr = "baron\@matrica.work" unless $from_addr;

my $headers = "From: $from_name <$from_addr>\015\012"
        . "To: $to\015\012"
        . "Subject: $subject\015\012";

# Add threading headers when replying to an existing message
if ($in_reply_to) {
    $headers .= "In-Reply-To: $in_reply_to\015\012"
            . "References: $in_reply_to\015\012";
}

# If body contains Content-Type as first line, move it to headers
if ($body =~ s/^(Content-Type:.*\015?\012)//) {
    $headers .= $1 . "MIME-Version: 1.0\015\012";
} else {
    $headers .= "MIME-Version: 1.0\015\012"
              . "Content-Type: text/plain; charset=UTF-8\015\012";
}

my $msg = $headers . "\015\012" . $body;

my $smtp = Net::SMTP->new("smtp-relay.brevo.com",
    Port => 587, Timeout => 30, Hello => "matrica.work")
    or die "connect: $!";

$smtp->auth("b35324001\@smtp-brevo.com",
    $ENV{BREVO_SMTP_KEY})
    or die "auth: " . $smtp->message;

$smtp->mail($from_addr)
    or die "mail from: " . $smtp->message;
$smtp->to($to)
    or die "rcpt to: " . $smtp->message;
$smtp->data()
    or die "data: " . $smtp->message;
$smtp->datasend($msg)
    or die "send data: " . $smtp->message;
$smtp->dataend()
    or die "data end: " . $smtp->message;
$smtp->quit;
print "Sent\n";
`
