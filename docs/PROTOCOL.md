# Sophos / Cyberoam captive-portal protocol

What autoportal's `sophos` driver speaks, as observed on a Sophos Firewall (SFOS) captive portal and
its `httpclient.js`. The portal is typically at `https://<firewall>:8090/httpclient.html`, with a
self-signed certificate.

All requests are `application/x-www-form-urlencoded` and relative to the portal root.

| Action | Request | Fields |
|---|---|---|
| Login | `POST login.xml` | `mode=191`, `username`, `password`, `a=<epoch ms>`, `producttype=0` (+ `state=<token>` after a CHALLENGE) |
| Logout | `POST logout.xml` | `mode=193`, `username`, `a=<epoch ms>`, `producttype=0` |
| Keep-alive | `GET live?mode=192&username=<lowercase>&a=<ms>&producttype=0` | |

`producttype`: `0` web browser, `1` iOS, `2` Android. `a` is a cache-buster; the server doesn't validate it.
The page's JavaScript doubles single quotes in the username (`'` → `''`) before encoding.

## Responses

```xml
<requestresponse>
  <status><![CDATA[LIVE]]></status>            <!-- LIVE = signed in, LOGIN = signed out/rejected, CHALLENGE -->
  <message><![CDATA[You are signed in as {username}]]></message>
  <state><![CDATA[]]></state>                  <!-- token to send back after CHALLENGE -->
</requestresponse>
```

- Messages may contain HTML entities (`You&#39;ve signed out`) and the `{username}` placeholder.
- A rejected login (wrong password, expired account, login limit) answers `LOGIN` with the reason in `<message>`.
- Logging in while already signed in answers `LIVE` again, so login is idempotent.

Keep-alive answers `<liverequestresponse><ack>…</ack></liverequestresponse>` with `ack`, `nack`,
`login_again` or `live_off`. Many portals disable it (`var keepaliverequest = 'N'` in the page, and `live_off`
replies), so autoportal doesn't depend on it. It checks real connectivity instead.

## What a signed-out client sees

Plain-HTTP requests to the internet are answered by the firewall's web proxy with
`403 Forbidden` and `Via: HTTP/1.1 forward.http.proxy:3128`, with **no redirect** to the portal. That's why
setup asks for (or discovers) the portal address instead of following a redirect.

## Detecting the portal

`GET httpclient.html` returns a page that loads `/javascript/cyberoamAjax.js` and
`/javascript/validation/httpclient.js`, and sets `var title = '…'`, which autoportal shows as the portal name.
