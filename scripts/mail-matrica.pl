#!/usr/bin/env perl
# mail-matrica.pl — unified mail toolbox: list and read emails
# Usage:
#   mail-matrica.pl ls [mailbox]          — list emails (date, from, subject)
#   mail-matrica.pl read <file> [mailbox] — show email content, save attachments
#   mail-matrica.pl --help                — show usage
use strict;
use warnings;
use open ":std", ":encoding(UTF-8)";
use Email::MIME;
use Encode;
use MIME::Base64;

my %mailboxes = (baron => '/srv/mail/baron', merchant => '/srv/mail/merchant');

sub mailbox_dir {
    my ($name) = @_;
    $name ||= 'baron';
    return $mailboxes{$name} if $mailboxes{$name};
    my $dir = "/srv/mail/$name";
    return -d $dir ? $dir : undef;
}

sub decode_mime {
    my ($v) = @_;
    return $v unless defined $v;
    return decode('MIME-Header', $v) if $v =~ /=\?/;
    return $v;
}

sub load_email {
    my ($path) = @_;
    open(my $fh, '<:raw', $path) or die "Can't open $path: $!\n";
    my $raw = do { local $/; <$fh> };
    close $fh;
    return Email::MIME->new(\$raw);
}

sub trunc {
    my ($s, $n) = @_;
    $s = substr($s, 0, $n) if length($s) > $n;
    return $s;
}

sub cmd_ls {
    my ($mailbox) = @_;
    my $dir = mailbox_dir($mailbox) or die "Mailbox '$mailbox' not found\n";
    opendir(my $dh, $dir) or die "Can't open $dir: $!\n";
    my @files = sort grep { /\.eml$/ && -f "$dir/$_" } readdir($dh);
    closedir $dh;

    printf "%-20s %-40s %-35s %s\n", "Date", "From", "File", "Subject";
    print "-" x 80, "\n";

    for my $f (reverse @files) {
        my $email = load_email("$dir/$f");
        my $from = decode_mime($email->header('From') || '');
        my $subject = decode_mime($email->header('Subject') || '');
        my $date_str = $email->header('Date') || '';
        my $date = $date_str;
        if ($date_str) {
            require Date::Parse;
            require POSIX;
            if (my $epoch = Date::Parse::str2time($date_str)) {
                $date = POSIX::strftime("%Y-%m-%d %H:%M", localtime($epoch));
            }
        }
        $from =~ s/\s+/ /g; $from =~ s/^\s+|\s+$//g;
        printf "%-20s %-40s %-35s %s\n",
            $date, trunc($from, 38), $f, trunc($subject, 40);
    }
}

my $MAILBOX = 'baron';
my $FNAME = '';

sub show_part {
    my ($part) = @_;
    my $ct = $part->content_type || 'text/plain';
    my $fn = $part->filename;

    if ($fn) {
        my $body = $part->body;
        my $outdir = "/srv/mail/$MAILBOX/attachments/$FNAME";
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

sub cmd_read {
    my ($fname, $mailbox) = @_;
    $fname || die "Usage: mail-matrica.pl read <file> [mailbox]\n";
    my $dir = mailbox_dir($mailbox) or die "Mailbox '$mailbox' not found\n";
    my $file = "$dir/$fname";
    die "File not found: $file\n" unless -f $file;

    my $email = load_email($file);
    for my $h (qw/From To Subject Date/) {
        my $v = decode_mime($email->header($h) || '');
        print "$h: $v\n";
    }
    print "-" x 60, "\n";

    $MAILBOX = $mailbox || 'baron';
    $FNAME = $fname;
    show_part($email);
}

my $cmd = shift @ARGV || 'ls';
if ($cmd eq '--help' || $cmd eq '-h' || $cmd eq 'help') {
    print "Usage:\n";
    print "  mail-matrica.pl ls [mailbox]\n";
    print "  mail-matrica.pl read <file> [mailbox]\n";
    exit 0;
}

if ($cmd eq 'ls') {
    cmd_ls(shift @ARGV);
} elsif ($cmd eq 'read') {
    cmd_read(shift @ARGV, shift @ARGV);
} else {
    die "Unknown command '$cmd'. Use ls or read (--help for usage).\n";
}
