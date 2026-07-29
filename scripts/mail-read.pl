#!/usr/bin/env perl
# mail-read.pl — show email content and save attachments
# Usage: mail-read.pl <filename> [mailbox]
use strict;
use open ":std", ":encoding(UTF-8)";
use Email::MIME;
use Encode;
use MIME::Base64;

my $fname = shift || die "Usage: mail-read.pl <filename> [mailbox]\n";
my $mailbox = shift || 'baron';
my $file = "/srv/mail/$mailbox/$fname";
die "File not found: $file\n" unless -f $file;

open(my $fh, '<:raw', $file) or die "Can't open: $!\n";
my $raw = do { local $/; <$fh> };
close $fh;

my $email = Email::MIME->new(\$raw);

# Headers
for my $h (qw/From To Subject Date/) {
    my $v = $email->header($h) || '';
    $v = decode('MIME-Header', $v) if $v =~ /=\?/;
    print "$h: $v\n";
}
print "-" x 60, "\n";

# Show body and save attachments
sub show_part {
    my ($part) = @_;
    my $ct = $part->content_type || 'text/plain';
    my $fn = $part->filename;

    if ($fn) {
        my $body = $part->body;
        my $outdir = "/srv/mail/$mailbox/attachments/$fname";
        system('mkdir', '-p', $outdir);
        my $path = "$outdir/$fn";
        open(my $out, '>:raw', $path) or warn "Can't save $fn: $!";
        print $out $body if $out;
        close $out if $out;
        print "📎 $fn ($path)\n";
    } elsif ($ct =~ m|text/plain|i) {
        my $text = $part->body_str;
        print $text;
        print "\n" unless $text =~ /\n$/;
    } elsif ($ct =~ m|multipart/|i) {
        for my $sub ($part->parts) {
            show_part($sub);
        }
    }
}

show_part($email);
