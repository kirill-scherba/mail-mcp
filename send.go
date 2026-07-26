// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package main

// sendPythonScript sends email via Brevo SMTP relay.
// Uses smtplib for sending. Python handles UTF-8 correctly.
var sendPythonScript = `
import smtplib
from email.message import EmailMessage
import sys

msg = EmailMessage()
msg.set_content(sys.argv[3])
msg['Subject'] = sys.argv[2]
msg['From'] = 'Барон <baron@matrica.work>'
msg['To'] = sys.argv[1]

server = smtplib.SMTP('smtp-relay.brevo.com', 587, timeout=30)
server.starttls()
server.login('b35324001@smtp-brevo.com', 'xsmtpsib-5aa3856f48f3f18cbb63f4d427df6f48dc269473c4fbfd38f39066af709b2476-WC2rT5sS5LzUy6o1')
server.send_message(msg)
server.quit()
print('Sent')
`
