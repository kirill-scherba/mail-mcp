// Cloudflare Worker — Email Routing → a-mail.matrica.work
// Deployed via Cloudflare Dashboard → Workers & Pages → calm-truth-0d5b
export default {
  async email(message, env, ctx) {
    const raw = await new Response(message.raw).text()
    await fetch('https://a-mail.matrica.work/mail/inbox', {
      method: 'POST',
      headers: {'Content-Type': 'message/rfc822'},
      body: raw
    })
  }
}
