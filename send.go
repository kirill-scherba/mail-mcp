// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package main

// sendPerlScript sends email via Brevo SMTP relay using Perl.
// Uses Net::SMTP with STARTTLS. No Python involved.
var sendPerlScript = `
use strict;
use Net::SMTP;

my ($to, $subject, $body, $api_key) = @ARGV;
die "Missing args" unless $to && $subject && $body && $api_key;

my $from_name = "Барон";
my $from_addr = "baron\@matrica.work";

my $msg = "From: $from_name <$from_addr>\015\012"
        . "To: $to\015\012"
        . "Subject: $subject\015\012"
        . "Content-Type: text/plain; charset=UTF-8\015\012"
        . "\015\012"
        . $body;

my $smtp = Net::SMTP->new("smtp-relay.brevo.com",
    Port => 587, Timeout => 30, Hello => "matrica.work")
    or die "connect: $!";

$smtp->auth("b35324001\@smtp-brevo.com",
    $api_key)
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
