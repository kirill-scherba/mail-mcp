#!/usr/bin/env perl
# mail-ls.pl — list emails in mailbox: date, from, subject
# Usage: mail-ls.pl [mailbox]
use strict;
use open ":std", ":encoding(UTF-8)";
use Email::MIME;
use Encode;
use Date::Parse;
use POSIX;

my $mailbox = $ARGV[0] || 'baron';
my $dir = "/srv/mail/$mailbox";
die "Mailbox $dir not found\n" unless -d $dir;

opendir(my $dh, $dir) or die "Can't open $dir: $!\n";
my @files = sort grep { /\.eml$/ && -f "$dir/$_" } readdir($dh);
closedir $dh;

printf "%-20s %-40s %-35s %s\n", "Date", "From", "File", "Subject";
printf "%s\n", '-' x 80;

for my $f (reverse @files) {
    open(my $fh, '<:raw', "$dir/$f") or next;
    my $raw = do { local $/; <$fh> };
    close $fh;

    my $email = Email::MIME->new(\$raw);
    my $from = $email->header('From') || '';
    my $subject = $email->header('Subject') || '';
    my $date_str = $email->header('Date') || '';

    # Format date
    my $date = '';
    if ($date_str && (my $epoch = str2time($date_str))) {
        $date = strftime("%Y-%m-%d %H:%M", localtime($epoch));
    }

    # Format from: Name <email>
    $from =~ s/\s+/ /g;
    $from =~ s/^\s+|\s+$//g;
    $from = decode('MIME-Header', $from) if $from =~ /=\?/;

    $from = substr($from, 0, 38) if length($from) > 38;
    $subject = decode('MIME-Header', $subject) if $subject =~ /=\?/;
    $subject = substr($subject, 0, 40) if length($subject) > 40;

    printf "%-20s %-40s %-35s %s\n", $date, $from, $f, $subject;
}
