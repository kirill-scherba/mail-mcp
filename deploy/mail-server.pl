#!/usr/bin/env perl
# mail-server.pl — Mail Matrix inbox server
# Copyright 2026 Kirill Scherba. All rights reserved.
# Use of this source code is governed by a BSD-style license.

use strict;
use IO::Socket::INET;
use Email::MIME;

my $port = 8901;
my $mail_dir = "/srv/mail";
mkdir $mail_dir unless -d $mail_dir;

my $socket = IO::Socket::INET->new(
    LocalPort => $port, Type => SOCK_STREAM,
    Reuse => 1, Listen => 5
) or die "Can't bind port $port: $!";

while (my $c = $socket->accept()) {
    $c->autoflush(1);
    my $req = <$c>;
    next unless $req;
    my ($method) = $req =~ /^(\w+)/;
    next unless $method eq 'POST';

    # Read HTTP headers
    my $ct_len = 0;
    while (<$c>) {
        last if $_ eq "\r\n" || $_ eq "\n";
        $ct_len = $1 if /Content-Length:\s*(\d+)/i;
    }

    # Read email body
    my $body = '';
    read($c, $body, $ct_len) if $ct_len > 0;

    if ($body && length($body) > 0) {
        # Determine mailbox from To: header
        my $mailbox = 'inbox';
        eval {
            my $email = Email::MIME->new($body);
            my $to = $email->header('To') || '';
            # Extract the local part from the To address
            if ($to =~ /([\w.-]+)\@matrica\.work/i) {
                $mailbox = lc($1);
            }
        };

        my $box_dir = "$mail_dir/$mailbox";
        mkdir $box_dir unless -d $box_dir;

        my @t = gmtime;
        my $fname = sprintf("%04d%02d%02d_%02d%02d%02d_$$",
            $t[5]+1900, $t[4]+1, $t[3], $t[2], $t[1], $t[0]);
        open(my $fh, '>', "$box_dir/$fname.eml") or warn $!;
        print $fh $body if $fh;
        close $fh if $fh;
    }

    print $c "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"ok\":true}";
    close($c);
}
