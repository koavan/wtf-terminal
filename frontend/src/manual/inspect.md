# Tokens and certificates

wtf can open what you find in the output: a JWT's claims, base64-encoded values, certificates, and the certificate chain of a TLS server. It shows facts (dates, names, keys) and doesn't judge them.

## JWTs

A JWT in the output (`eyJ…`) is a link: {{mac:⌘-click|other:Ctrl-click}} it, or right-click it and choose **Inspect Token…**. You can also select one and right-click → **Inspect JWT…**.

The viewer shows when it expires (`exp`), when it was issued (`iat`), when it becomes valid (`nbf`), the issuer, subject and audience, then the full claims and header. **The signature isn't verified**: wtf has no key to check it with, so "not expired" says nothing about whether the token is genuine.

## Base64

Select base64 text and right-click → **Decode Base64…**. In `kubectl get secret -o yaml` you don't need to select anything: right-click the value after a key (`password: cGFzc3…`).

Decoded values are often secrets, so the result is hidden until you click **Reveal**, and hidden again when wtf loses focus. The viewer tells you what it decoded to: text, JSON (indented), binary data (a hex dump of the first 256 bytes), a PEM certificate (**Inspect Certificate…**) or a JWT (**Inspect JWT…**). A `tls.crt` from a Kubernetes secret is two clicks from its expiry date.

## Certificates

- **A file:** right-click a `.pem`, `.crt`, `.cer` or `.der` file in [Files](#files) → **Inspect Certificate…**. Every certificate in it is shown, in order. A private key in the same file is noted but never shown.
- **Selected text:** select a `-----BEGIN CERTIFICATE-----` block (indented YAML is fine) and right-click → **Inspect Certificate…**.

For each certificate: subject, issuer, names (DNS names, IPs, emails, URIs), when it's valid and how long is left, the key, the signature algorithm, the serial number and the SHA-256 fingerprint (with **Copy**), and whether it's a CA or self-signed.

## TLS servers

Three ways in:

- Right-click an `https://` URL in the output → **Inspect TLS Certificate…**.
- Select a host name, `host:port` or IP and right-click → **Inspect TLS Certificate of …**.
- From the command palette: **Inspect TLS Certificate…**, then type a host.

wtf connects from this computer (port 443 unless you give one) and shows the protocol, cipher and ALPN it agreed on, and the chain the server sent: server, intermediates, root. It accepts any certificate so broken ones can be seen, then says whether the chain **verifies** against this computer's trusted roots for that host name. When it doesn't, it shows the TLS library's own error (expired, unknown authority, wrong name…). Only the handshake is sent: no HTTP request, no cookies, no credentials.

## With Hide Secrets on

While [Hide Secrets](#secrets) is on, JWTs and base64 values can't be opened: decoding them would show what's being hidden. Certificates and TLS servers can still be inspected; they're public.
